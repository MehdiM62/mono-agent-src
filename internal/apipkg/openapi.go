package apipkg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/monoes/mono-agent/internal/connections"
)

// OpenAPIOptions tunes OpenAPIToPackage.
type OpenAPIOptions struct {
	// ID is the package id (default: derived from info.title).
	ID string
	// Tags keeps only operations with one of these tags (default: all).
	Tags []string
	// Warn receives what could not be converted (optional).
	Warn func(string)
}

// spec is a parsed OpenAPI 3.0/3.1 or Swagger 2.0 document.
type spec struct {
	doc     map[string]any
	swagger bool
	opts    OpenAPIOptions
}

// OpenAPIToPackage converts an OpenAPI 3.0/3.1 or Swagger 2.0 document
// (JSON or YAML) to a package: tags become resources, operationIds
// operations, parameters and request bodies fields, security schemes
// credentials. The result passes Parse.
func OpenAPIToPackage(data []byte, opts OpenAPIOptions) (*Package, error) {
	doc, err := decodeSpec(data)
	if err != nil {
		return nil, err
	}
	s := &spec{doc: doc, opts: opts}
	switch {
	case strings.HasPrefix(str(doc["openapi"]), "3."):
	case str(doc["swagger"]) == "2.0":
		s.swagger = true
	default:
		return nil, fmt.Errorf("openapi: not an OpenAPI 3.x or Swagger 2.0 document (no \"openapi\": \"3.x\" or \"swagger\": \"2.0\")")
	}
	info := obj(doc["info"])
	title := strings.TrimSpace(str(info["title"]))
	if title == "" {
		title = "API"
	}
	id := opts.ID
	if id == "" {
		id = snake(title)
	}
	if !regexp.MustCompile(`^[a-z0-9_]+$`).MatchString(id) {
		return nil, fmt.Errorf("openapi: package id %q must match [a-z0-9_]+ (pass --id)", id)
	}
	p := &Package{
		Schema: SchemaID, ID: id, NodeType: NodeTypePrefix + id, Title: title,
		Description: firstLine(str(info["description"])), Category: "app",
		BaseURL: s.baseURL(),
	}
	if ed := obj(doc["externalDocs"]); ed != nil {
		p.DocsURL = str(ed["url"])
	}
	p.Credentials = s.credentials(id)
	s.operations(p)
	if len(p.Operations) == 0 {
		return nil, fmt.Errorf("openapi: no operations to convert (check --ops tags)")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(b, "converted "+id); err != nil {
		return nil, fmt.Errorf("openapi: the converted package does not validate: %w", err)
	}
	return p, nil
}

func (s *spec) warn(format string, a ...any) {
	if s.opts.Warn != nil {
		s.opts.Warn(fmt.Sprintf(format, a...))
	}
}

// decodeSpec reads JSON, else YAML, into plain maps with string keys.
func decodeSpec(data []byte) (map[string]any, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		if yerr := yaml.Unmarshal(data, &v); yerr != nil {
			return nil, fmt.Errorf("openapi: neither JSON (%v) nor YAML (%v)", err, yerr)
		}
	}
	m, ok := plain(v).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("openapi: the document is not an object")
	}
	return m, nil
}

// plain turns YAML's map[any]any into map[string]any, recursively.
func plain(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, el := range t {
			t[k] = plain(el)
		}
		return t
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, el := range t {
			m[fmt.Sprint(k)] = plain(el)
		}
		return m
	case []any:
		for i, el := range t {
			t[i] = plain(el)
		}
		return t
	case int:
		return float64(t)
	}
	return v
}

// resolve follows a local "$ref" (#/…) once or more.
func (s *spec) resolve(v any) map[string]any {
	m := obj(v)
	for i := 0; m != nil && i < 16; i++ {
		ref := str(m["$ref"])
		if ref == "" {
			return m
		}
		if !strings.HasPrefix(ref, "#/") {
			s.warn("external $ref %q is not followed", ref)
			return nil
		}
		var cur any = s.doc
		for _, seg := range strings.Split(ref[2:], "/") {
			seg = strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
			cur = obj(cur)[seg]
		}
		m = obj(cur)
	}
	return m
}

func (s *spec) baseURL() string {
	if s.swagger {
		host := str(s.doc["host"])
		if host == "" {
			s.warn("no host: set base_url by hand")
			host = "api.example.com"
		}
		scheme := "https"
		if sc, ok := s.doc["schemes"].([]any); ok && len(sc) > 0 && !containsAny(sc, "https") {
			scheme = str(sc[0])
		}
		return scheme + "://" + host + strings.TrimRight(str(s.doc["basePath"]), "/")
	}
	servers, _ := s.doc["servers"].([]any)
	if len(servers) == 0 {
		s.warn("no servers: set base_url by hand")
		return "https://api.example.com"
	}
	sv := obj(servers[0])
	u := str(sv["url"])
	for name, raw := range obj(sv["variables"]) {
		u = strings.ReplaceAll(u, "{"+name+"}", str(obj(raw)["default"]))
	}
	if !strings.HasPrefix(u, "http") {
		s.warn("server url %q is relative: prefix it with the API's host in base_url", u)
	}
	return strings.TrimRight(u, "/")
}

// credentials converts security schemes, one credential per scheme.
func (s *spec) credentials(id string) []Credential {
	schemes := obj(obj(s.doc["components"])["securitySchemes"])
	if s.swagger {
		schemes = obj(s.doc["securityDefinitions"])
	}
	base := id
	if connections.IsBuiltin(base) {
		base += "_api"
	}
	var out []Credential
	for _, name := range sortedKeys(schemes) {
		sc := s.resolve(schemes[name])
		platform := base
		if len(out) > 0 {
			platform = base + "_" + snake(name)
		}
		c := Credential{Platform: platform, Name: name}
		bearer := func() {
			c.Methods = []string{"bearer"}
			c.Fields = []CredentialField{{Key: "access_token", Label: "Access token", Secret: true, Required: true}}
			c.Inject.Headers = map[string]string{"Authorization": "Bearer {{ cred.access_token }}"}
		}
		switch str(sc["type"]) {
		case "http":
			if strings.EqualFold(str(sc["scheme"]), "basic") {
				c.Methods = []string{"basic"}
				c.Fields = []CredentialField{{Key: "username", Label: "Username", Required: true},
					{Key: "password", Label: "Password", Secret: true, Required: true}}
				c.Inject.Basic = &BasicInject{Username: "{{ cred.username }}", Password: "{{ cred.password }}"}
			} else {
				bearer()
			}
		case "basic":
			c.Methods = []string{"basic"}
			c.Fields = []CredentialField{{Key: "username", Label: "Username", Required: true},
				{Key: "password", Label: "Password", Secret: true, Required: true}}
			c.Inject.Basic = &BasicInject{Username: "{{ cred.username }}", Password: "{{ cred.password }}"}
		case "apiKey":
			c.Methods = []string{"api_key"}
			c.Fields = []CredentialField{{Key: "api_key", Label: str(sc["name"]), Secret: true, Required: true}}
			switch str(sc["in"]) {
			case "query":
				c.Inject.Query = map[string]string{str(sc["name"]): "{{ cred.api_key }}"}
			case "cookie":
				c.Inject.Headers = map[string]string{"Cookie": str(sc["name"]) + "={{ cred.api_key }}"}
			default:
				c.Inject.Headers = map[string]string{str(sc["name"]): "{{ cred.api_key }}"}
			}
		case "oauth2":
			auth, token, scopes := s.oauthFlow(sc)
			if auth == "" || token == "" {
				s.warn("security scheme %q: no authorization-code flow; using a bearer token", name)
				bearer()
				break
			}
			c.Methods = []string{"oauth"}
			c.OAuth = &OAuthSpec{AuthURL: auth, TokenURL: token, Scopes: scopes, PKCE: true}
			c.Inject.Headers = map[string]string{"Authorization": "Bearer {{ cred.access_token }}"}
		default:
			s.warn("security scheme %q (%s): using a bearer token", name, str(sc["type"]))
			bearer()
		}
		out = append(out, c)
	}
	return out
}

func (s *spec) oauthFlow(sc map[string]any) (auth, token string, scopes []string) {
	var flow map[string]any
	if s.swagger {
		if str(sc["flow"]) == "accessCode" {
			flow = sc
		}
	} else {
		flow = obj(obj(sc["flows"])["authorizationCode"])
	}
	if flow == nil {
		return "", "", nil
	}
	return str(flow["authorizationUrl"]), str(flow["tokenUrl"]), sortedKeys(obj(flow["scopes"]))
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string         { s, _ := v.(string); return s }

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// words splits an identifier or phrase into lower-case words.
func words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if unicode.IsUpper(r) && len(cur) > 0 && (unicode.IsLower(cur[len(cur)-1]) ||
				(i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(cur[len(cur)-1]))) {
				flush()
			}
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return out
}

func snake(s string) string { return strings.Join(words(s), "_") }

// camel is lowerCamelCase, n8n's style for values and param names.
func camel(s string) string {
	ws := words(s)
	for i := 1; i < len(ws); i++ {
		ws[i] = strings.ToUpper(ws[i][:1]) + ws[i][1:]
	}
	out := strings.Join(ws, "")
	if out == "" || unicode.IsDigit(rune(out[0])) {
		out = "x" + out
	}
	return out
}
