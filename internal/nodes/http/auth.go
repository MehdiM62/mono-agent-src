package httpnodes

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/connections"
)

// authPlan is how one request authenticates. At most one mechanism is set.
// Its secret values are never logged or put in an error message.
type authPlan struct {
	basic   bool
	digest  bool
	user    string
	pw      string
	tok     string // sent as "Authorization: Bearer <tok>"
	hdrName string
	hdrVal  string
	qName   string
	qVal    string
	oauth1  *connections.OAuth1Params // signs the request (OAuth 1.0a)
}

// authTypes are the auth_type values http.request and http.graphql accept.
var authTypes = []string{"none", "basic", "bearer", "api_key", "digest", "header", "query", "credential"}

// authTypeAliases maps n8n's authentication / genericAuthType names onto
// ours, so an imported node keeps working without a rewrite.
var authTypeAliases = map[string]string{
	"httpBasicAuth":            "basic",
	"basicAuth":                "basic",
	"httpDigestAuth":           "digest",
	"digestAuth":               "digest",
	"httpHeaderAuth":           "header",
	"headerAuth":               "header",
	"httpQueryAuth":            "query",
	"queryAuth":                "query",
	"httpBearerAuth":           "bearer",
	"predefinedCredentialType": "credential",
	"oAuth2":                   "credential",
	"oAuth2Api":                "credential",
	"oAuth1":                   "credential",
	"oAuth1Api":                "credential",
}

// credentialSecretKeys are the connection fields that hold a single API
// secret, in the order they are tried.
var credentialSecretKeys = []string{"access_token", "api_key", "token", "secret_key", "api_token", "auth_token", "bot_token"}

// credentialBasicPairs are the connection field pairs that are a username
// and password: generic basic auth, Atlassian (email + API token), Twilio
// (account SID + auth token).
var credentialBasicPairs = [][2]string{
	{"username", "password"},
	{"email", "api_token"},
	{"account_sid", "auth_token"},
	{"user", "password"},
}

// buildAuthPlan works out how to authenticate from the node config. When a
// saved connection is selected (credential_id, which the engine resolves
// into config["credential"]) and auth_type is none, empty or "credential",
// the connection decides; otherwise the auth_* fields do.
func buildAuthPlan(config map[string]interface{}, node string) (authPlan, error) {
	authType, err := oneOf(config, "auth_type", "none", authTypes, authTypeAliases)
	if err != nil {
		return authPlan{}, fmt.Errorf("%s: %w", node, err)
	}
	cred, hasCred := config["credential"].(map[string]interface{})
	credID := strings.TrimSpace(configString(config, "credential_id"))

	if authType == "credential" || (authType == "none" && hasCred && credID != "") {
		if !hasCred {
			if credID == "" {
				return authPlan{}, fmt.Errorf("%s: auth_type is \"credential\" but no credential_id is set", node)
			}
			return authPlan{}, fmt.Errorf("%s: credential_id %q did not resolve to a saved connection in this profile", node, credID)
		}
		plan, err := credentialAuth(cred, configString(config, "credential_auth"))
		if err != nil {
			return authPlan{}, fmt.Errorf("%s: credential_id %q: %w", node, credID, err)
		}
		return plan, nil
	}

	var plan authPlan
	switch authType {
	case "basic", "digest":
		plan.basic = authType == "basic"
		plan.digest = authType == "digest"
		plan.user = configString(config, "auth_username")
		plan.pw = configString(config, "auth_password")
	case "bearer":
		plan.tok = configString(config, "auth_token")
	case "api_key", "header", "query":
		in := configString(config, "auth_api_key_in")
		if authType != "api_key" {
			in = authType
		}
		name := configString(config, "auth_api_key_name")
		value := configString(config, "auth_api_key_value")
		if name == "" {
			return authPlan{}, fmt.Errorf("%s: auth_api_key_name is required for auth_type %q", node, authType)
		}
		switch in {
		case "", "header":
			if err := validHeaderName(name); err != nil {
				return authPlan{}, fmt.Errorf("%s: auth_api_key_name: %w", node, err)
			}
			plan.hdrName, plan.hdrVal = name, value
		case "query":
			plan.qName, plan.qVal = name, value
		default:
			return authPlan{}, fmt.Errorf("%s: auth_api_key_in: unknown value %q (use header or query)", node, in)
		}
	}
	return plan, nil
}

// credentialAuth derives the auth plan from a saved connection's data.
//
// With no override: an OAuth 1.0a connection (consumer_key and
// oauth_token) signs the request; an OAuth access_token is sent as a Bearer
// token; a username/password pair (or email + api_token, account_sid + auth_token)
// as Basic auth; a single API key or token as a Bearer token — or in the
// header the connection names (generic_api's header_name).
//
// override is credential_auth: "bearer", "basic", "oauth1",
// "header:<Name>" or "query:<name>".
func credentialAuth(cred map[string]interface{}, override string) (authPlan, error) {
	var plan authPlan
	str := func(k string) string { s, _ := cred[k].(string); return strings.TrimSpace(s) }

	secret := ""
	for _, k := range credentialSecretKeys {
		if v := str(k); v != "" {
			secret = v
			break
		}
	}
	userPass := func() (string, string, bool) {
		for _, p := range credentialBasicPairs {
			if u, pw := str(p[0]), str(p[1]); u != "" && pw != "" {
				return u, pw, true
			}
		}
		return "", "", false
	}

	override = strings.TrimSpace(override)
	kind, name, _ := strings.Cut(override, ":")
	kind = strings.ToLower(strings.TrimSpace(kind))
	name = strings.TrimSpace(name)

	switch kind {
	case "":
		if str(connections.OAuth1ConsumerKey) != "" && str(connections.OAuth1Token) != "" {
			return oauth1Auth(str)
		}
		if t := str("access_token"); t != "" {
			plan.tok = t
			return plan, nil
		}
		if u, pw, ok := userPass(); ok {
			plan.basic, plan.user, plan.pw = true, u, pw
			return plan, nil
		}
		if secret == "" {
			return authPlan{}, fmt.Errorf("the connection has no access_token, API key/token or username and password to authenticate with (fields present: %s); set credential_auth or use auth_type", fieldNames(cred))
		}
		if h := str("header_name"); h != "" && !strings.EqualFold(h, "Authorization") {
			if err := validHeaderName(h); err != nil {
				return authPlan{}, fmt.Errorf("connection header_name: %w", err)
			}
			plan.hdrName, plan.hdrVal = h, secret
			return plan, nil
		}
		plan.tok = secret
		return plan, nil
	case "oauth1":
		if str(connections.OAuth1ConsumerKey) == "" {
			return authPlan{}, fmt.Errorf("credential_auth oauth1: the connection has no consumer_key (fields present: %s)", fieldNames(cred))
		}
		return oauth1Auth(str)
	case "bearer":
		if secret == "" {
			return authPlan{}, fmt.Errorf("credential_auth bearer: the connection has no access_token, API key or token (fields present: %s)", fieldNames(cred))
		}
		plan.tok = secret
	case "basic":
		if u, pw, ok := userPass(); ok {
			plan.user, plan.pw = u, pw
		} else if secret != "" {
			// Stripe-style: the key is the username, the password is empty.
			plan.user = secret
		} else {
			return authPlan{}, fmt.Errorf("credential_auth basic: the connection has no username and password or key (fields present: %s)", fieldNames(cred))
		}
		plan.basic = true
	case "header", "query":
		if name == "" {
			return authPlan{}, fmt.Errorf("credential_auth %q: name the %s, e.g. %s:X-API-Key", override, kind, kind)
		}
		if secret == "" {
			return authPlan{}, fmt.Errorf("credential_auth %s: the connection has no API key or token (fields present: %s)", kind, fieldNames(cred))
		}
		if kind == "header" {
			if err := validHeaderName(name); err != nil {
				return authPlan{}, fmt.Errorf("credential_auth: %w", err)
			}
			plan.hdrName, plan.hdrVal = name, secret
		} else {
			plan.qName, plan.qVal = name, secret
		}
	default:
		return authPlan{}, fmt.Errorf("credential_auth: unknown value %q (use bearer, basic, oauth1, header:<Name> or query:<name>)", override)
	}
	return plan, nil
}

// oauth1Auth signs with an OAuth 1.0a connection's consumer and token
// pair. Its signature_method (HMAC-SHA1 when unset) and realm are the
// connection's fields too.
func oauth1Auth(str func(string) string) (authPlan, error) {
	method := strings.ToUpper(str(connections.OAuth1SignatureMethod))
	switch method {
	case "", "HMAC-SHA1", "HMAC-SHA256", "HMAC-SHA512", "PLAINTEXT":
	default:
		return authPlan{}, fmt.Errorf("the connection's signature_method %q is not HMAC-SHA1, HMAC-SHA256, HMAC-SHA512 or PLAINTEXT", method)
	}
	return authPlan{oauth1: &connections.OAuth1Params{
		ConsumerKey:     str(connections.OAuth1ConsumerKey),
		ConsumerSecret:  str(connections.OAuth1ConsumerSecret),
		Token:           str(connections.OAuth1Token),
		TokenSecret:     str(connections.OAuth1TokenSecret),
		SignatureMethod: method,
		Realm:           str("realm"),
	}}, nil
}

// fieldNames lists a connection's field names (never values) for errors.
func fieldNames(cred map[string]interface{}) string {
	names := make([]string, 0, len(cred))
	for k := range cred {
		names = append(names, k)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// validHeaderName rejects header names net/http would refuse to send.
func validHeaderName(name string) error {
	if name == "" || strings.ContainsAny(name, " \t\r\n:") {
		return fmt.Errorf("invalid header name %q", name)
	}
	return nil
}

// applyToQuery adds a query-string credential.
func (p authPlan) applyToQuery(q url.Values) {
	if p.qName != "" {
		q.Set(p.qName, p.qVal)
	}
}

// applyToRequest sets the auth headers; an OAuth 1.0a signature covers
// body when it is a form. Digest is applied by the sender after the
// server's challenge.
func (p authPlan) applyToRequest(req *http.Request, body []byte) error {
	switch {
	case p.oauth1 != nil:
		return connections.SignOAuth1(req, body, *p.oauth1)
	case p.basic:
		req.SetBasicAuth(p.user, p.pw)
	case p.tok != "":
		req.Header.Set("Authorization", "Bearer "+p.tok)
	case p.hdrName != "":
		req.Header.Set(p.hdrName, p.hdrVal)
	}
	return nil
}

// credentialBaseURL returns the base URL a saved connection supplies
// (generic_api / generic_basic base_url, Salesforce instance_url), used to
// complete a url that is only a path.
func credentialBaseURL(config map[string]interface{}) string {
	cred, ok := config["credential"].(map[string]interface{})
	if !ok {
		return ""
	}
	for _, k := range []string{"base_url", "instance_url"} {
		if s, _ := cred[k].(string); strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// resolveRequestURL parses rawURL, completing a bare path ("/v1/items")
// against the selected connection's base URL.
func resolveRequestURL(rawURL string, config map[string]interface{}, field string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("'%s' is required", field)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", field, err)
	}
	if u.Scheme == "" && u.Host == "" {
		base := credentialBaseURL(config)
		if base == "" {
			return nil, fmt.Errorf("%s: %q is not an absolute URL (add https://, or select a connection that has a base URL)", field, rawURL)
		}
		b, err := url.Parse(strings.TrimRight(base, "/") + "/")
		if err != nil {
			return nil, fmt.Errorf("%s: connection base URL: %v", field, err)
		}
		u = b.ResolveReference(&url.URL{Path: strings.TrimLeft(u.Path, "/"), RawQuery: u.RawQuery, Fragment: u.Fragment})
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%s: unsupported scheme %q (use http or https)", field, u.Scheme)
	}
	return u, nil
}
