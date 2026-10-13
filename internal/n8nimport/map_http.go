package n8nimport

import (
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	Register("n8n-nodes-base.httpRequest", MapHTTPRequest)
}

// MapHTTPRequest maps every HttpRequest version onto http.request. It is
// exported so the http.request owner can register a richer mapper over it
// (Register("n8n-nodes-base.httpRequest", …)) once that node grows n8n's
// remaining options; this one targets the config keys http.request reads
// today: method, url, query_params, headers, body, body_type, auth_*,
// timeout, follow_redirects, response_format.
func MapHTTPRequest(c *MapContext) Mapping {
	cfg := map[string]interface{}{}
	if c.version(3) {
		httpV3(c, cfg)
	} else {
		httpV1(c, cfg)
	}
	httpAuth(c, cfg)
	return Mapping{Type: "http.request", Config: cfg}
}

var monoMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true}

func httpMethod(c *MapContext, param string, cfg map[string]interface{}) {
	m := strings.ToUpper(c.Literal(param, "GET"))
	if !monoMethods[m] {
		c.Warn(param, m, "http.request supports GET, POST, PUT, PATCH, DELETE and HEAD; GET is used")
		m = "GET"
	}
	cfg["method"] = m
}

// pairs turns n8n name/value lists into a map with translated values.
func (c *MapContext) pairs(param string, list []map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for i, p := range list {
		name := fmt.Sprint(p["name"])
		if t, _ := p["parameterType"].(string); t == "formBinaryData" {
			c.Warn(fmt.Sprintf("%s[%d]", param, i), name, "binary form fields are not supported")
			continue
		}
		out[name] = c.Translate(fmt.Sprintf("%s.%s", param, name), p["value"])
	}
	return out
}

// jsonParam returns a JSON-text parameter as an object when it is static
// JSON, or as translated template text (mono parses JSON-looking results).
func (c *MapContext) jsonParam(param string) interface{} {
	raw := c.Raw(param)
	if s, ok := raw.(string); ok && !strings.HasPrefix(s, "=") {
		var v interface{}
		if json.Unmarshal([]byte(s), &v) == nil {
			return escapeDeep(v)
		}
	}
	return c.Translate(param, raw)
}

func httpV3(c *MapContext, cfg map[string]interface{}) {
	httpMethod(c, "method", cfg)
	cfg["url"] = c.Str("url", "")
	if c.Bool("sendQuery", false) {
		if c.Literal("specifyQuery", "keypair") == "json" {
			cfg["query_params"] = c.jsonParam("jsonQuery")
		} else {
			cfg["query_params"] = c.pairs("queryParameters", c.paramList("queryParameters.parameters"))
		}
	}
	c.Ignore("specifyQuery", "queryParameters", "jsonQuery")
	if c.Bool("sendHeaders", false) {
		if c.Literal("specifyHeaders", "keypair") == "json" {
			cfg["headers"] = c.jsonParam("jsonHeaders")
		} else {
			cfg["headers"] = c.pairs("headerParameters", c.paramList("headerParameters.parameters"))
		}
	}
	c.Ignore("specifyHeaders", "headerParameters", "jsonHeaders")
	if c.Bool("sendBody", false) {
		httpBodyV3(c, cfg)
	}
	c.Ignore("contentType", "specifyBody", "bodyParameters", "jsonBody", "body", "rawContentType", "inputDataFieldName")
	if t := c.Raw("options.timeout"); t != nil {
		cfg["timeout"] = msToSeconds(c.Float("options.timeout", 10000))
	}
	if c.Has("options.redirect") {
		cfg["follow_redirects"] = c.Bool("options.redirect.redirect.followRedirects", c.version(4))
		c.Ignore("options.redirect.redirect.maxRedirects")
	} else if !c.version(4) {
		cfg["follow_redirects"] = false
	}
	if c.Has("options.response") {
		switch f := c.Literal("options.response.response.responseFormat", "autodetect"); f {
		case "text":
			cfg["response_format"] = "text"
		case "file":
			cfg["response_format"] = "binary"
		case "json":
			cfg["response_format"] = "json"
		}
		if c.Bool("options.response.response.fullResponse", false) {
			c.Warn("options.response.fullResponse", true, "http.request returns the body only")
		}
		if c.Bool("options.response.response.neverError", false) {
			c.Note("neverError: use the node's on_error setting to continue on HTTP errors")
		}
		c.Ignore("options.response.response.outputPropertyName")
	}
	if c.Has("options.pagination") {
		c.Unsupported("options.pagination")
	}
	if c.Bool("options.allowUnauthorizedCerts", false) {
		c.Unsupported("options.allowUnauthorizedCerts")
	}
	c.Ignore("options.batching", "options.lowercaseHeaders", "options.queryParameterArrays", "options.sendCredentialsOnCrossOriginRedirect")
	if c.Has("options.proxy") {
		c.Unsupported("options.proxy")
	}
}

func msToSeconds(ms float64) float64 {
	s := ms / 1000
	if s < 1 {
		s = 1
	}
	return s
}

func httpBodyV3(c *MapContext, cfg map[string]interface{}) {
	ct := c.Literal("contentType", "json")
	switch ct {
	case "json":
		cfg["body_type"] = "json"
		if c.Literal("specifyBody", "keypair") == "json" {
			cfg["body"] = c.jsonParam("jsonBody")
		} else {
			cfg["body"] = c.pairs("bodyParameters", c.paramList("bodyParameters.parameters"))
		}
	case "form-urlencoded", "multipart-form-data":
		cfg["body_type"] = "form"
		if ct == "multipart-form-data" {
			c.Warn("contentType", ct, "is sent as application/x-www-form-urlencoded")
		}
		if c.Literal("specifyBody", "keypair") == "string" {
			cfg["body"] = c.Str("body", "")
		} else {
			cfg["body"] = c.pairs("bodyParameters", c.paramList("bodyParameters.parameters"))
		}
	case "raw":
		cfg["body_type"] = "text"
		cfg["body"] = c.Str("body", "")
		if rt := c.Str("rawContentType", ""); rt != "" {
			h, _ := cfg["headers"].(map[string]interface{})
			if h == nil {
				h = map[string]interface{}{}
			}
			h["Content-Type"] = rt
			cfg["headers"] = h
			c.Note("raw bodies are sent with Content-Type text/plain unless the header map overrides it")
		}
	case "binaryData":
		c.Warn("contentType", ct, "binary request bodies are not supported by http.request")
	}
}

func httpV1(c *MapContext, cfg map[string]interface{}) {
	httpMethod(c, "requestMethod", cfg)
	cfg["url"] = c.Str("url", "")
	switch f := c.Literal("responseFormat", "json"); f {
	case "string":
		cfg["response_format"] = "text"
	case "file":
		cfg["response_format"] = "binary"
	}
	c.Ignore("dataPropertyName")
	if c.Bool("jsonParameters", false) {
		for param, key := range map[string]string{"queryParametersJson": "query_params", "headerParametersJson": "headers", "bodyParametersJson": "body"} {
			if c.Has(param) {
				cfg[key] = c.jsonParam(param)
			}
		}
		if c.Bool("sendBinaryData", false) {
			c.Unsupported("sendBinaryData")
		}
		c.Ignore("binaryPropertyName")
	} else {
		if l := c.paramList("queryParametersUi.parameter"); len(l) > 0 {
			cfg["query_params"] = c.pairs("queryParametersUi", l)
		}
		if l := c.paramList("headerParametersUi.parameter"); len(l) > 0 {
			cfg["headers"] = c.pairs("headerParametersUi", l)
		}
		if l := c.paramList("bodyParametersUi.parameter"); len(l) > 0 {
			cfg["body"] = c.pairs("bodyParametersUi", l)
		}
	}
	c.Ignore("queryParametersUi", "headerParametersUi", "bodyParametersUi")
	if _, ok := cfg["body"]; ok {
		switch bt := c.Literal("options.bodyContentType", "json"); bt {
		case "form-urlencoded", "multipart-form-data":
			cfg["body_type"] = "form"
		case "raw":
			cfg["body_type"] = "text"
		default:
			cfg["body_type"] = "json"
		}
	}
	if c.Has("options.timeout") {
		cfg["timeout"] = msToSeconds(c.Float("options.timeout", 10000))
	}
	if c.Has("options.followRedirect") {
		cfg["follow_redirects"] = c.Bool("options.followRedirect", true)
	}
	for _, o := range []string{"options.fullResponse", "options.ignoreResponseCode", "options.proxy", "options.splitIntoItems"} {
		if c.Has(o) {
			c.Unsupported(o)
		}
	}
	c.Ignore("options.batchInterval", "options.batchSize", "options.useQueryString", "options.followAllRedirects", "options.bodyContentCustomMimeType")
	if c.Bool("allowUnauthorizedCerts", false) {
		c.Unsupported("allowUnauthorizedCerts")
	}
}

// httpAuth maps n8n's authentication onto http.request's auth fields. The
// secrets themselves are never in an n8n export, so they come from vault
// secrets the report lists.
func httpAuth(c *MapContext, cfg map[string]interface{}) {
	auth := c.Literal("authentication", "none")
	kind := auth
	switch auth {
	case "genericCredentialType":
		kind = c.Literal("genericAuthType", "")
	case "predefinedCredentialType":
		kind = c.Literal("nodeCredentialType", "")
	case "basicAuth", "headerAuth", "queryAuth", "digestAuth", "oAuth1", "oAuth2":
		kind = map[string]string{"basicAuth": "httpBasicAuth", "headerAuth": "httpHeaderAuth", "queryAuth": "httpQueryAuth",
			"digestAuth": "httpDigestAuth", "oAuth1": "oAuth1Api", "oAuth2": "oAuth2Api"}[auth]
	}
	switch kind {
	case "none", "":
	case "httpBasicAuth":
		cfg["auth_type"] = "basic"
		cfg["auth_username"] = c.Secret("username", kind)
		cfg["auth_password"] = c.Secret("password", kind)
	case "httpBearerAuth":
		cfg["auth_type"] = "bearer"
		cfg["auth_token"] = c.Secret("token", kind)
	case "httpHeaderAuth", "httpQueryAuth":
		cfg["auth_type"] = "api_key"
		cfg["auth_api_key_in"] = map[string]string{"httpHeaderAuth": "header", "httpQueryAuth": "query"}[kind]
		cfg["auth_api_key_name"] = ""
		cfg["auth_api_key_value"] = c.Secret("value", kind)
		c.Warn("authentication", kind, "set auth_api_key_name to the name used in the n8n credential")
	default:
		cfg["auth_type"] = "bearer"
		cfg["auth_token"] = c.Secret("token", kind)
		c.Warn("authentication", kind, "is sent as a bearer token from a vault secret; check the API accepts that")
	}
}
