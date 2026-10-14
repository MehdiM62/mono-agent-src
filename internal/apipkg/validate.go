package apipkg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/connections"
)

// ValidationError lists every problem found in one package file.
type ValidationError struct {
	Source   string
	Problems []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("apipkg: %s is invalid:\n  - %s", e.Source, strings.Join(e.Problems, "\n  - "))
}

// reservedFieldKeys are config keys the engine or the workflow owns.
var reservedFieldKeys = map[string]bool{
	"resource": true, "operation": true, "credential": true, "credential_id": true,
	"on_error": true, "retry": true,
}

// Parse validates a package file (schema, then semantics) and decodes it.
// source names the file in error messages.
func Parse(b []byte, source string) (*Package, error) {
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, &ValidationError{Source: source, Problems: []string{"not valid JSON: " + err.Error()}}
	}
	sv, err := packageSchema()
	if err != nil {
		return nil, err
	}
	if probs := sv.validate(doc); len(probs) > 0 {
		return nil, &ValidationError{Source: source, Problems: probs}
	}
	var p Package
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, &ValidationError{Source: source, Problems: []string{err.Error()}}
	}
	if probs := p.check(); len(probs) > 0 {
		return nil, &ValidationError{Source: source, Problems: probs}
	}
	p.Source = source
	p.defaultRequiredSelects()
	return &p, nil
}

// check runs the semantic checks the JSON schema cannot express.
func (p *Package) check() []string {
	var probs []string
	add := func(format string, a ...any) { probs = append(probs, fmt.Sprintf(format, a...)) }

	if p.Extends != "" && p.NodeType != "" {
		add("set node_type or extends, not both")
	}
	resources := map[string]bool{}
	for _, r := range p.Resources {
		if resources[r.Value] {
			add("resources: %q is listed twice", r.Value)
		}
		resources[r.Value] = true
	}

	credKeys := map[string]bool{}
	for _, k := range oauthTokenKeys {
		credKeys[k] = true
	}
	for i := range p.Credentials {
		probs = append(probs, p.Credentials[i].check(i)...)
		for _, f := range p.Credentials[i].Fields {
			credKeys[f.Key] = true
		}
		if o := p.Credentials[i].OAuth; o != nil {
			for _, k := range o.storedKeys() {
				credKeys[k] = true
			}
		}
	}
	probs = append(probs, checkTemplates("base_url", p.BaseURL, nil, credKeys)...)
	probs = append(probs, checkHeaderTemplates("package", p.Headers, nil, credKeys)...)
	probs = append(probs, checkTemplates("error_when", []any{p.ErrorWhen, p.ErrorMessage}, anyField, credKeys)...)
	probs = append(probs, checkOutputDecls("outputs", p.Outputs)...)

	if p.Extends != "" {
		native := nativeOperations(p.Extends)
		for _, o := range p.Operations {
			if o.Resource == "" && native[o.Operation] {
				add("operation %q has no resource and %s already has an operation with that name: give it a resource (or another name), or it would replace the native one", o.Operation, p.Extends)
			}
		}
	}
	if len(p.Operations) == 0 && len(p.N8nTriggerTypes) == 0 {
		add("operations: a package needs operations (or, for a trigger-only app, n8n_trigger_types)")
	}
	seen := map[string]bool{}
	used := map[string]bool{}
	for i := range p.Operations {
		o := &p.Operations[i]
		where := fmt.Sprintf("operations[%d] (%s)", i, o.Key())
		if seen[o.Key()] {
			add("%s: operation %q is defined twice", where, o.Key())
		}
		seen[o.Key()] = true
		used[o.Resource] = true
		if len(p.Resources) > 0 && !resources[o.Resource] {
			add("%s: resource %q is not in resources[] (have %s)", where, o.Resource, resourceList(p.Resources))
		}
		if len(p.Resources) == 0 && o.Resource != "" {
			add("%s: resource %q set but the package has no resources[]", where, o.Resource)
		}
		probs = append(probs, o.check(where, credKeys)...)
		probs = append(probs, checkOutputDecls(where+": outputs", o.Outputs)...)
	}
	for _, r := range p.Resources {
		if !used[r.Value] {
			add("resources: %q has no operations", r.Value)
		}
	}
	for k := range p.N8nVersionDefaults {
		if !versionKeyRE.MatchString(k) {
			add("n8n_version_defaults: key %q must be a typeVersion (\"2\", \"2.1\") or a comparison (\"<3\", \">=2\")", k)
		}
	}
	probs = append(probs, p.checkScopeGroups()...)
	if p.Webhook != nil {
		probs = append(probs, p.Webhook.check(credKeys)...)
	}
	return probs
}

// versionKeyRE matches an n8n_version_defaults key: an exact typeVersion or
// a comparison (<, <=, >, >=) the importer applies to every matching version.
var versionKeyRE = regexp.MustCompile(`^(<=|>=|<|>)?\s*[0-9]+(\.[0-9]+)?$`)

func resourceList(rs []Resource) string {
	vals := make([]string, len(rs))
	for i, r := range rs {
		vals[i] = r.Value
	}
	return strings.Join(vals, ", ")
}

func (c *Credential) check(i int) []string {
	var probs []string
	where := fmt.Sprintf("credentials[%d] (%s)", i, c.Platform)
	add := func(format string, a ...any) { probs = append(probs, where+": "+fmt.Sprintf(format, a...)) }
	if c.ReusePlatform != "" {
		if !connections.IsBuiltin(c.ReusePlatform) {
			add("reuse_platform %q is not a built-in connection platform", c.ReusePlatform)
		}
	} else if connections.IsBuiltin(c.Platform) {
		add("platform %q is a built-in connection platform: set reuse_platform to use it", c.Platform)
	}
	onlyNone := true
	for _, m := range c.Methods {
		if m != "none" {
			onlyNone = false
		}
		if m == "oauth" && c.ReusePlatform == "" && (c.OAuth == nil || c.OAuth.AuthURL == "") {
			add(`method "oauth" needs an "oauth" block (auth_url, token_url)`)
		}
		if m == "client_credentials" {
			if c.OAuth == nil || c.OAuth.TokenURL == "" {
				add(`method "client_credentials" needs oauth.token_url`)
			} else {
				idf, secf := c.OAuth.clientFields()
				have := map[string]bool{}
				for _, f := range c.Fields {
					have[f.Key] = true
				}
				if !have[idf] || !have[secf] {
					add("method \"client_credentials\" needs fields %q and %q (or set oauth.client_id_field / client_secret_field)", idf, secf)
				}
			}
		}
		if m == "oauth1" && (c.OAuth == nil || c.OAuth.RequestTokenURL == "" || c.OAuth.AuthURL == "" || c.OAuth.TokenURL == "") {
			add(`method "oauth1" needs oauth.request_token_url, auth_url and token_url (the access-token endpoint)`)
		}
		if (m == "api_key" || m == "bearer" || m == "basic" || m == "client_credentials" || m == "oauth1") && len(c.Fields) == 0 && c.ReusePlatform == "" {
			add("method %q needs fields[] (what the user types in)", m)
		}
	}
	keys := map[string]bool{}
	for _, k := range oauthTokenKeys {
		keys[k] = true
	}
	if c.OAuth != nil {
		for _, k := range c.OAuth.storedKeys() {
			keys[k] = true
		}
	}
	for _, f := range c.Fields {
		if keys[f.Key] && f.Key != "access_token" && !(c.OAuth != nil && contains(c.OAuth.TokenFields, f.Key)) {
			add("field %q clashes with an OAuth token key", f.Key)
		}
		keys[f.Key] = true
	}
	if o := c.OAuth; o != nil {
		for _, k := range o.ConnectFields {
			if !keys[k] || contains(oauthTokenKeys, k) {
				add("oauth.connect_fields: %q is not a field of this credential", k)
			}
		}
		tmpls := []any{o.AuthURL, o.TokenURL}
		for _, v := range o.ExtraParams {
			tmpls = append(tmpls, v)
		}
		for _, v := range o.Scopes {
			tmpls = append(tmpls, v)
		}
		if m := o.Metadata; m != nil {
			tmpls = append(tmpls, m.URL)
			for _, v := range m.Headers {
				tmpls = append(tmpls, v)
			}
			if len(m.Fields) == 0 {
				add("oauth.metadata needs fields (connection key → response path)")
			}
		}
		probs = append(probs, checkTemplates(where+" oauth", tmpls, nil, keys)...)
	}
	in := c.Inject
	if !onlyNone && len(in.Headers) == 0 && len(in.Query) == 0 && in.Basic == nil && in.AWSSigV4 == nil && in.JWT == nil && len(in.Body) == 0 && in.HMAC == nil && in.AzureSharedKey == nil && in.AzureCosmos == nil && in.OAuth1 == nil {
		add("inject is empty: say how the credential authenticates (headers, query, basic or aws_sigv4)")
	}
	for k, v := range in.Headers {
		probs = append(probs, checkTemplates(where+" inject.headers."+k, v, anyField, keys)...)
	}
	for k, v := range in.Query {
		probs = append(probs, checkTemplates(where+" inject.query."+k, v, anyField, keys)...)
	}
	if b := in.Basic; b != nil {
		probs = append(probs, checkTemplates(where+" inject.basic", []any{b.Username, b.Password}, anyField, keys)...)
	}
	if o := in.OAuth1; o != nil {
		probs = append(probs, checkTemplates(where+" inject.oauth1",
			[]any{o.ConsumerKey, o.ConsumerSecret, o.Token, o.TokenSecret, o.Realm}, anyField, keys)...)
	}
	if s := in.AWSSigV4; s != nil {
		probs = append(probs, checkTemplates(where+" inject.aws_sigv4",
			[]any{s.Service, s.Region, s.AccessKeyID, s.SecretAccessKey, s.SessionToken}, anyField, keys)...)
	}
	for k, v := range in.Body {
		probs = append(probs, checkTemplates(where+" inject.body."+k, v, anyField, keys)...)
	}
	if jw := in.JWT; jw != nil {
		switch jw.Alg {
		case "HS256", "HS384", "HS512", "RS256", "ES256":
		default:
			add("inject.jwt.alg %q: use HS256, HS384, HS512, RS256 or ES256", jw.Alg)
		}
		if jw.Secret == "" {
			add("inject.jwt needs a secret template")
		}
		claims := []any{jw.Secret, jw.Kid}
		for _, v := range jw.Claims {
			claims = append(claims, v)
		}
		probs = append(probs, checkTemplates(where+" inject.jwt", claims, anyField, keys)...)
	}
	return probs
}

var allowedFieldTypes = map[string]bool{
	"text": true, "textarea": true, "number": true, "boolean": true, "select": true,
	"multiselect": true, "json": true, "collection": true, "datetime": true,
	"password": true, "code": true, "array": true,
}
