package n8nimport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/connections"
	httpnodes "github.com/monoes/mono-agent/internal/nodes/http"
)

func init() {
	Register("n8n-nodes-base.httpRequest", MapHTTPRequest)
	Register("n8n-nodes-base.graphql", MapGraphQL)
}

// MapHTTPRequest maps every HttpRequest version onto http.request. Version
// 3+ follows internal/nodes/http's N8nHTTPParamMap: http.request implements
// n8n's options (pagination, batching, proxy, full response, …) and
// accepts n8n's option spellings. n8n's defaults that differ from ours are
// always written: full_response false, response_format autodetect,
// lowercase_headers true, timeout converted from ms to seconds.
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

// httpCopy copies an n8n parameter that maps one-to-one, mapping its
// option values through the table.
func httpCopy(c *MapContext, cfg map[string]interface{}, table map[string]httpnodes.N8nParam, path string) {
	p, ok := table[path]
	if !ok || p.Key == "" || !c.Has(path) {
		return
	}
	v := c.Value(path)
	if s, isStr := v.(string); isStr {
		if to, ok := p.Values[s]; ok && to != "" {
			v = to
		}
	}
	cfg[p.Key] = v
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

// pairs turns n8n name/value rows into an object with translated values.
func (c *MapContext) pairs(param string, list []map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for _, p := range list {
		name := fmt.Sprint(p["name"])
		out[name] = c.Translate(fmt.Sprintf("%s.%s", param, name), p["value"])
	}
	return out
}

// httpV3Plain are the version 3+ parameters copied through the table.
var httpV3Plain = []string{
	"options.batching.batch.batchSize", "options.batching.batch.batchInterval",
	"options.allowUnauthorizedCerts", "options.queryParameterArrays",
	"options.redirect.redirect.maxRedirects", "options.response.response.neverError",
	"options.response.response.outputPropertyName", "options.proxy",
	"options.sendCredentialsOnCrossOriginRedirect",
	"options.pagination.pagination.paginationMode", "options.pagination.pagination.nextURL",
	"options.pagination.pagination.parameters.parameters", "options.pagination.pagination.paginationCompleteWhen",
	"options.pagination.pagination.statusCodesWhenComplete", "options.pagination.pagination.completeExpression",
	"options.pagination.pagination.requestInterval",
}

func httpV3(c *MapContext, cfg map[string]interface{}) {
	t := httpnodes.N8nHTTPParamMap
	cfg["method"] = strings.ToUpper(c.Literal("method", "GET"))
	cfg["url"] = c.Str("url", "")
	if c.Bool("sendQuery", false) {
		if c.Literal("specifyQuery", "keypair") == "json" {
			cfg["query_params"] = c.jsonParam("jsonQuery")
		} else if c.Has("queryParameters.parameters") {
			cfg["query_params"] = c.Value("queryParameters.parameters")
		}
	}
	c.Ignore("specifyQuery", "queryParameters", "jsonQuery")
	if c.Bool("sendHeaders", false) {
		if c.Literal("specifyHeaders", "keypair") == "json" {
			cfg["headers"] = c.jsonParam("jsonHeaders")
		} else if c.Has("headerParameters.parameters") {
			cfg["headers"] = c.Value("headerParameters.parameters")
		}
	}
	c.Ignore("specifyHeaders", "headerParameters", "jsonHeaders")
	if c.Bool("sendBody", false) {
		httpBodyV3(c, cfg)
	}
	c.Ignore("contentType", "specifyBody", "bodyParameters", "jsonBody", "body", "rawContentType", "inputDataFieldName")

	for _, p := range httpV3Plain {
		httpCopy(c, cfg, t, p)
	}
	if c.Bool("options.pagination.pagination.limitPagesFetched", false) {
		httpCopy(c, cfg, t, "options.pagination.pagination.maxRequests")
	}
	c.Ignore("options.pagination.pagination.limitPagesFetched", "options.pagination.pagination.maxRequests")
	if c.Has("options.timeout") {
		cfg["timeout"] = msToSeconds(c.Float("options.timeout", 10000))
	}
	if c.Has("options.redirect.redirect.followRedirects") {
		cfg["follow_redirects"] = c.Bool("options.redirect.redirect.followRedirects", true)
	} else {
		cfg["follow_redirects"] = c.version(4) // n8n 3 did not follow redirects by default
	}
	// n8n's defaults where ours differ.
	cfg["lowercase_headers"] = c.Bool("options.lowercaseHeaders", true)
	cfg["full_response"] = c.Bool("options.response.response.fullResponse", false)
	format := c.Literal("options.response.response.responseFormat", "autodetect")
	if to, ok := t["options.response.response.responseFormat"].Values[format]; ok {
		format = to
	}
	cfg["response_format"] = format
	c.Ignore("curlImport", "provideSslCertificates", "infoMessage", "googleApiWarning",
		"provideSslCertificatesNotice", "options.webdavMethods")
	if c.Bool("provideSslCertificates", false) {
		c.Warn("provideSslCertificates", true, "client certificates are not supported")
	}
}

// msToSeconds converts n8n's millisecond timeout to our seconds (at least 1).
func msToSeconds(ms float64) float64 {
	s := ms / 1000
	if s < 1 {
		s = 1
	}
	return s
}

func httpBodyV3(c *MapContext, cfg map[string]interface{}) {
	ct := c.Literal("contentType", "json")
	keypair := c.Literal("specifyBody", "keypair")
	switch ct {
	case "json":
		cfg["body_type"] = "json"
		if keypair == "json" {
			cfg["body"] = c.jsonParam("jsonBody")
		} else {
			cfg["body"] = c.pairs("bodyParameters", c.paramList("bodyParameters.parameters"))
		}
	case "form-urlencoded", "multipart-form-data":
		cfg["body_type"] = map[string]string{"form-urlencoded": "form", "multipart-form-data": "multipart"}[ct]
		if keypair == "string" {
			cfg["body"] = c.Str("body", "")
		} else if c.Has("bodyParameters.parameters") {
			cfg["body"] = c.Value("bodyParameters.parameters")
		}
	case "raw":
		cfg["body_type"] = "raw"
		cfg["body"] = c.Str("body", "")
		if rt := c.Str("rawContentType", ""); rt != "" {
			cfg["raw_content_type"] = rt
		}
	case "binaryData":
		cfg["body_type"] = "binary"
		cfg["binary_property"] = c.Literal("inputDataFieldName", "data")
	default:
		c.Warn("contentType", ct, "unknown body content type; the body is sent as JSON")
		cfg["body_type"] = "json"
	}
}

func httpV1(c *MapContext, cfg map[string]interface{}) {
	cfg["method"] = strings.ToUpper(c.Literal("requestMethod", "GET"))
	cfg["url"] = c.Str("url", "")
	switch f := c.Literal("responseFormat", "json"); f {
	case "string":
		cfg["response_format"] = "text"
	case "file":
		cfg["response_format"] = "file"
		cfg["output_property"] = c.Literal("dataPropertyName", "data")
	default:
		cfg["response_format"] = "json"
	}
	c.Ignore("dataPropertyName")
	if c.Bool("jsonParameters", false) {
		for param, key := range map[string]string{"queryParametersJson": "query_params", "headerParametersJson": "headers", "bodyParametersJson": "body"} {
			if c.Has(param) {
				cfg[key] = c.jsonParam(param)
			}
		}
		if c.Bool("sendBinaryData", false) {
			cfg["body_type"] = "binary"
			cfg["binary_property"] = c.Literal("binaryPropertyName", "data")
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
	if _, ok := cfg["body"]; ok && cfg["body_type"] == nil {
		switch bt := c.Literal("options.bodyContentType", "json"); bt {
		case "form-urlencoded":
			cfg["body_type"] = "form"
		case "multipart-form-data":
			cfg["body_type"] = "multipart"
		case "raw":
			cfg["body_type"] = "raw"
		default:
			cfg["body_type"] = "json"
		}
	}
	if c.Has("options.timeout") {
		cfg["timeout"] = msToSeconds(c.Float("options.timeout", 10000))
	}
	cfg["follow_redirects"] = c.Bool("options.followRedirect", true)
	cfg["full_response"] = c.Bool("options.fullResponse", false)
	cfg["never_error"] = c.Bool("options.ignoreResponseCode", false)
	cfg["allow_unauthorized_certs"] = c.Bool("allowUnauthorizedCerts", false)
	if c.Has("options.proxy") {
		cfg["proxy"] = c.Str("options.proxy", "")
	}
	if c.Has("options.batchSize") {
		cfg["batch_size"] = c.Value("options.batchSize")
	}
	if c.Has("options.batchInterval") {
		cfg["batch_interval_ms"] = c.Value("options.batchInterval")
	}
	if c.Has("options.splitIntoItems") {
		c.Unsupported("options.splitIntoItems")
	}
	c.Ignore("options.useQueryString", "options.followAllRedirects", "options.bodyContentCustomMimeType")
}

// httpAuth maps n8n's authentication onto http.request's (and
// http.graphql's) auth fields. A predefined credential type, Basic, Header,
// Bearer and OAuth1 auth use a saved connection; the secrets of the others come
// from vault secrets the report lists (n8n exports never carry them).
func httpAuth(c *MapContext, cfg map[string]interface{}) {
	auth := c.Literal("authentication", "none")
	kind := auth
	switch auth {
	case "genericCredentialType":
		kind = c.Literal("genericAuthType", "")
	case "predefinedCredentialType":
		credType := c.Literal("nodeCredentialType", "")
		cfg["auth_type"] = "credential"
		cfg["credential_id"] = c.Credential(credentialPlatformFor(credType), credType)
		return
	case "basicAuth", "headerAuth", "queryAuth", "digestAuth", "oAuth1", "oAuth2":
		kind = map[string]string{"basicAuth": "httpBasicAuth", "headerAuth": "httpHeaderAuth", "queryAuth": "httpQueryAuth",
			"digestAuth": "httpDigestAuth", "oAuth1": "oAuth1Api", "oAuth2": "oAuth2Api"}[auth]
	}
	useConnection := func(platform, hint string) {
		cfg["auth_type"] = "credential"
		cfg["credential_id"] = c.Credential(platform, kind)
		c.creds[len(c.creds)-1].Hint = hint
	}
	switch kind {
	case "none", "":
	case "httpBasicAuth":
		useConnection("generic_basic", "monoagentcli connect generic_basic (username and password from the n8n Basic Auth credential)")
	case "httpHeaderAuth":
		useConnection("generic_api", "monoagentcli connect generic_api (header_name = the n8n Header Auth credential's Name, api_key = its Value)")
	case "httpBearerAuth":
		useConnection("generic_api", "monoagentcli connect generic_api (api_key = the bearer token)")
	case "oAuth2Api":
		useConnection("oauth2", "create an OAuth connection for this API and set credential_id to it")
	case "oAuth1Api":
		useConnection("generic_oauth1", "monoagentcli connect oauth generic_oauth1 --field request_token_url=… --field authorization_url=… --field access_token_url=… (the n8n OAuth1 credential's URLs; consumer key, secret and signature method are asked, or pass them as --field too)")
	case "httpDigestAuth":
		cfg["auth_type"] = "digest"
		cfg["auth_username"] = c.Secret("username", kind)
		cfg["auth_password"] = c.Secret("password", kind)
	case "httpQueryAuth":
		cfg["auth_type"] = "query"
		cfg["auth_api_key_name"] = ""
		cfg["auth_api_key_value"] = c.Secret("value", kind)
		c.Warn("authentication", kind, "set auth_api_key_name to the query parameter name used in the n8n credential")
	default:
		cfg["auth_type"] = "bearer"
		cfg["auth_token"] = c.Secret("token", kind)
		c.Warn("authentication", kind, "has no equivalent; it is sent as a bearer token from a vault secret, check the API accepts that")
	}
}

// credentialPlatformFor names the connection platform for an n8n
// credential type: an API package's or the Google / Microsoft mapping,
// else the type without its Api / OAuth2Api suffix.
func credentialPlatformFor(credType string) string {
	// The connection registry's mapping wins: it names the product platform
	// a native node resolves (gmail finds a gmail or a google connection),
	// where an API package may name the umbrella it reuses.
	if m, ok := connections.N8nCredentials[credType]; ok && m.Platform != "" {
		return m.Platform
	}
	refreshAppSources()
	regMu.RLock()
	var found string
	for _, m := range []map[string]AppMapper{apps, srcApps} {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if p, ok := m[k].CredentialPlatform[credType]; ok && found == "" {
				found = p
			}
		}
	}
	regMu.RUnlock()
	if found != "" {
		return found
	}
	if m, ok := connections.N8nCredentials[credType]; ok {
		return m.Platform
	}
	p := credType
	for _, suffix := range []string{"OAuth2Api", "OAuth2", "Api"} {
		if strings.HasSuffix(p, suffix) && len(p) > len(suffix) {
			p = strings.TrimSuffix(p, suffix)
			break
		}
	}
	return strings.ToLower(p)
}

// MapGraphQL maps the GraphQL node onto http.graphql through
// internal/nodes/http's N8nGraphQLParamMap.
func MapGraphQL(c *MapContext) Mapping {
	t := httpnodes.N8nGraphQLParamMap
	cfg := map[string]interface{}{}
	for _, p := range []string{"endpoint", "query", "operationName", "dataPropertyName", "allowUnauthorizedCerts"} {
		httpCopy(c, cfg, t, p)
	}
	cfg["request_method"] = strings.ToLower(c.Literal("requestMethod", "POST"))
	def := "json"
	if !c.version(1.1) {
		def = "graphql"
	}
	cfg["request_format"] = c.Literal("requestFormat", def)
	format := c.Literal("responseFormat", "json")
	if to, ok := t["responseFormat"].Values[format]; ok {
		format = to
	}
	cfg["response_format"] = format
	if c.Has("variables") {
		cfg["variables"] = c.jsonParam("variables")
	}
	if c.Has("headerParametersUi.parameter") {
		cfg["headers"] = c.Value("headerParametersUi.parameter")
	}
	c.Ignore("headerParametersUi")
	httpAuth(c, cfg)
	return Mapping{Type: "http.graphql", Config: cfg}
}
