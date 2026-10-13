package apipkg

import (
	"encoding/json"
	"fmt"
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
	}
	probs = append(probs, checkTemplates("base_url", p.BaseURL, nil, credKeys)...)
	for k, v := range p.Headers {
		probs = append(probs, checkTemplates("headers."+k, v, nil, credKeys)...)
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
	}
	for _, r := range p.Resources {
		if !used[r.Value] {
			add("resources: %q has no operations", r.Value)
		}
	}
	return probs
}

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
		if (m == "api_key" || m == "bearer" || m == "basic" || m == "client_credentials") && len(c.Fields) == 0 && c.ReusePlatform == "" {
			add("method %q needs fields[] (what the user types in)", m)
		}
	}
	keys := map[string]bool{}
	for _, k := range oauthTokenKeys {
		keys[k] = true
	}
	for _, f := range c.Fields {
		if keys[f.Key] && f.Key != "access_token" {
			add("field %q clashes with an OAuth token key", f.Key)
		}
		keys[f.Key] = true
	}
	in := c.Inject
	if !onlyNone && len(in.Headers) == 0 && len(in.Query) == 0 && in.Basic == nil && in.AWSSigV4 == nil {
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
	if s := in.AWSSigV4; s != nil {
		probs = append(probs, checkTemplates(where+" inject.aws_sigv4",
			[]any{s.Service, s.Region, s.AccessKeyID, s.SecretAccessKey, s.SessionToken}, anyField, keys)...)
	}
	return probs
}

var allowedFieldTypes = map[string]bool{
	"text": true, "textarea": true, "number": true, "boolean": true, "select": true,
	"multiselect": true, "json": true, "collection": true, "datetime": true,
	"password": true, "code": true, "array": true,
}

func (o *Operation) check(where string, credKeys map[string]bool) []string {
	var probs []string
	add := func(format string, a ...any) { probs = append(probs, where+": "+fmt.Sprintf(format, a...)) }
	fields := map[string]bool{"resource": true, "operation": true}
	for _, f := range o.Fields {
		if reservedFieldKeys[f.Key] {
			add("field key %q is reserved", f.Key)
		}
		if fields[f.Key] && !reservedFieldKeys[f.Key] {
			add("field %q is defined twice", f.Key)
		}
		fields[f.Key] = true
		if !allowedFieldTypes[f.Type] {
			add("field %q: unknown type %q", f.Key, f.Type)
		}
		if (f.Type == "select" || f.Type == "multiselect") && len(f.Options) == 0 {
			add("field %q: a %s needs options", f.Key, f.Type)
		}
	}
	if !strings.HasPrefix(o.Path, "/") && !strings.HasPrefix(o.Path, "{{") && !strings.HasPrefix(o.Path, "http") {
		add("path %q must start with / (or be an absolute URL)", o.Path)
	}
	probs = append(probs, checkTemplates(where+" base_url", o.BaseURL, fields, credKeys)...)
	probs = append(probs, checkTemplates(where+" path", o.Path, fields, credKeys)...)
	probs = append(probs, checkTemplates(where+" query", o.Query, fields, credKeys)...)
	probs = append(probs, checkTemplates(where+" body", o.Body, fields, credKeys)...)
	for k, v := range o.Headers {
		probs = append(probs, checkTemplates(where+" headers."+k, v, fields, credKeys)...)
	}
	if o.Output != nil && o.Output.Expand != nil {
		st := o.Output.Expand
		for _, v := range []any{st.When, st.BaseURL, st.Path, st.Query, st.Body} {
			probs = append(probs, checkTemplates(where+" output.expand", v, fields, credKeys)...)
		}
	}
	for i, st := range o.Before {
		if !validMethods[st.Method] || st.Path == "" {
			add("before[%d]: needs a method (GET, POST, …) and a path", i)
		}
		for _, v := range []any{st.When, st.BaseURL, st.Path, st.Query, st.Body} {
			probs = append(probs, checkTemplates(where+" before", v, fields, credKeys)...)
		}
	}
	for i, st := range o.Also {
		sw := fmt.Sprintf("%s also[%d]", where, i)
		if !validMethods[st.Method] {
			add("also[%d]: method %q must be GET, POST, PUT, PATCH, DELETE or HEAD", i, st.Method)
		}
		if st.Path == "" {
			add("also[%d]: path is required", i)
		}
		for _, v := range []any{st.When, st.BaseURL, st.Path, st.Query, st.Body} {
			probs = append(probs, checkTemplates(sw, v, fields, credKeys)...)
		}
		for k, v := range st.Headers {
			probs = append(probs, checkTemplates(sw+" headers."+k, v, fields, credKeys)...)
		}
	}
	bt := o.effectiveBodyType()
	if (bt == BodyForm || bt == BodyMultipart) && o.Body != nil {
		if _, ok := o.Body.(map[string]any); !ok {
			add("a %s body must be an object", bt)
		}
	}
	if bt == BodyNone && o.Body != nil {
		add(`body is set but body_type is "none"`)
	}
	if pg := o.Pagination; pg != nil {
		need := map[string][2]string{
			PageCursor:  {pg.CursorParam + "|" + pg.CursorPath, "cursor_param and cursor_path"},
			PageOffset:  {pg.OffsetParam, "offset_param"},
			PagePage:    {pg.PageParam, "page_param"},
			PageNextURL: {pg.NextURLPath, "next_url_path"},
		}
		if n, ok := need[pg.Type]; ok && (n[0] == "" || strings.HasPrefix(n[0], "|") || strings.HasSuffix(n[0], "|")) {
			add("pagination %q needs %s", pg.Type, n[1])
		}
		if pg.ParamIn == "body" && bt != BodyJSON {
			add(`pagination param_in "body" needs a json body`)
		}
	}
	if b := o.Binary; b != nil {
		for _, f := range []string{b.UploadField, b.PropertyField} {
			if f != "" && !fields[f] {
				add("binary: %q is not one of the op's fields", f)
			}
		}
		probs = append(probs, checkTemplates(where+" binary", []any{b.FileName, b.MimeType}, fields, credKeys)...)
	}
	for n8n, ours := range o.N8nParamMap {
		if !fields[ours] {
			add("n8n_param_map: %q maps to %q, which is not one of the op's fields", n8n, ours)
		}
	}
	return probs
}

// checkTemplates parses every template string in v and checks that p.X
// names a field (when fields is set) and cred.X a credential key.
var validMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true}

func checkTemplates(where string, v any, fields, credKeys map[string]bool) []string {
	var probs []string
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if !hasTemplate(t) {
				return
			}
			tm, err := parseTemplate(t)
			if err != nil {
				probs = append(probs, fmt.Sprintf("%s: %v", where, err))
				return
			}
			for _, part := range tm.parts {
				if part.expr == nil {
					continue
				}
				exprs := []*expr{part.expr}
				for _, f := range part.expr.filters {
					for _, a := range f.args {
						if a != f.arg && a.path != nil {
							exprs = append(exprs, a.path)
						}
						if a.sub != nil {
							exprs = append(exprs, a.sub)
						}
					}
					if f.arg != nil && f.arg.sub != nil && len(f.args) == 0 {
						exprs = append(exprs, f.arg.sub)
					}
					if f.arg != nil && f.arg.path != nil {
						exprs = append(exprs, f.arg.path)
					}
				}
				for _, e := range exprs {
					probs = append(probs, checkRef(where, e, fields, credKeys)...)
				}
			}
		case map[string]any:
			for k, el := range t {
				walk(k)
				walk(el)
			}
		case []any:
			for _, el := range t {
				walk(el)
			}
		}
	}
	walk(v)
	return probs
}

// anyField marks templates (credential inject) that may read any op
// param: they are shared by every op, so p.* can't be checked per op.
var anyField = map[string]bool{"*": true}

func checkRef(where string, e *expr, fields, credKeys map[string]bool) []string {
	if len(e.path) == 0 {
		return nil
	}
	switch e.root {
	case "p":
		if fields == nil {
			return []string{fmt.Sprintf("%s: {{ %s }}: p.* is not available here (only cred.*)", where, e.src)}
		}
		if !fields[e.path[0]] && !fields["*"] {
			return []string{fmt.Sprintf("%s: {{ %s }}: %q is not one of the op's fields", where, e.src, e.path[0])}
		}
	case "cred":
		if !credKeys[e.path[0]] {
			return []string{fmt.Sprintf("%s: {{ %s }}: %q is not a credential field", where, e.src, e.path[0])}
		}
	case "item", "el", "res", "items", "pre", "pres", "bin":
		if fields == nil {
			return []string{fmt.Sprintf("%s: {{ %s }}: %s.* is not available here (only cred.*)", where, e.src, e.root)}
		}
	}
	return nil
}
