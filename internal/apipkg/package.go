// Package apipkg runs declarative API packages ("apipkg.v1"): JSON files
// that describe a REST API's operations, credentials and pagination, so an
// app node is data instead of hand-written Go. One engine executes every
// package; see FORMAT.md for the author guide.
package apipkg

import (
	"bytes"
	"encoding/json"
	"github.com/monoes/mono-agent/internal/workflow"
)

// SchemaID is the value of "$schema" in every package file.
const SchemaID = "apipkg.v1"

// NodeTypePrefix is the namespace of the node types packages register.
const NodeTypePrefix = "app."

// Package is one API package file.
type Package struct {
	Schema      string            `json:"$schema"`
	ID          string            `json:"id"`
	NodeType    string            `json:"node_type,omitempty"`
	Extends     string            `json:"extends,omitempty"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Category    string            `json:"category,omitempty"`
	DocsURL     string            `json:"docs_url,omitempty"`
	N8nTypes    []string          `json:"n8n_types,omitempty"`
	BaseURL     string            `json:"base_url"`
	Headers     map[string]string `json:"headers,omitempty"`
	Credentials []Credential      `json:"credentials,omitempty"`
	Resources   []Resource        `json:"resources,omitempty"`
	Operations  []Operation       `json:"operations"`

	// N8nTriggerTypes are n8n trigger node types of the same app. Triggers
	// are not supported yet; the importer reports them as such.
	N8nTriggerTypes []string `json:"n8n_trigger_types,omitempty"`

	// Source is where the package was loaded from ("catalog" or a file
	// path). Not part of the file.
	Source string `json:"-"`
}

// Resource is one n8n "resource" option.
type Resource struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Credential describes how a package authenticates.
type Credential struct {
	Platform      string            `json:"platform"`
	Name          string            `json:"name,omitempty"`
	N8nCredential string            `json:"n8n_credential,omitempty"`
	Methods       []string          `json:"methods"`
	Fields        []CredentialField `json:"fields,omitempty"`
	OAuth         *OAuthSpec        `json:"oauth,omitempty"`
	Inject        Inject            `json:"inject"`
	ReusePlatform string            `json:"reuse_platform,omitempty"`
	// ScopeGroup names the umbrella-connection scope group the package
	// needs (google: calendar, docs, forms, …; microsoft: mail, files, …).
	// Before the first request the engine checks the connection grants it
	// and fails with the exact `connect … --scopes` command when not.
	ScopeGroup string `json:"scope_group,omitempty"`

	// N8nCredentialAlt are further n8n credential types that map to this
	// credential (e.g. the OAuth2 variant of an API-key credential).
	N8nCredentialAlt []string `json:"n8n_credential_alt,omitempty"`
	// N8nFieldMap renames n8n credential fields to ours (n8n → ours).
	N8nFieldMap map[string]string `json:"n8n_field_map,omitempty"`
}

// CredentialField is one input of a credential form.
type CredentialField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
	Help     string `json:"help,omitempty"`
}

// OAuthSpec configures an OAuth 2.0 authorization-code flow (always PKCE).
type OAuthSpec struct {
	AuthURL     string            `json:"auth_url,omitempty"`
	TokenURL    string            `json:"token_url"`
	Scopes      []string          `json:"scopes,omitempty"`
	PKCE        bool              `json:"pkce,omitempty"`
	ExtraParams map[string]string `json:"extra_params,omitempty"`
	// Client-credentials grant (method "client_credentials"): which
	// credential fields hold the client id/secret (default client_id /
	// client_secret) and how they are sent: "body" (default) or "basic".
	// token_url and extra_params values are templates over cred.*.
	ClientIDField     string `json:"client_id_field,omitempty"`
	ClientSecretField string `json:"client_secret_field,omitempty"`
	ClientAuth        string `json:"client_auth,omitempty"`
}

// Inject says how a credential authenticates a request. Every value is a
// template over cred.*.
type Inject struct {
	Headers  map[string]string `json:"headers,omitempty"`
	Query    map[string]string `json:"query,omitempty"`
	Basic    *BasicInject      `json:"basic,omitempty"`
	AWSSigV4 *AWSSigV4Inject   `json:"aws_sigv4,omitempty"`
}

// BasicInject is HTTP basic authentication.
type BasicInject struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AWSSigV4Inject signs requests with AWS Signature Version 4. Keys come from
// cred.access_key_id, cred.secret_access_key and cred.session_token unless
// overridden.
type AWSSigV4Inject struct {
	Service         string `json:"service"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	SessionToken    string `json:"session_token,omitempty"`
}

// Operation is one (resource, operation) pair and the request it makes.
type Operation struct {
	Resource    string                     `json:"resource"`
	Operation   string                     `json:"operation"`
	Title       string                     `json:"title"`
	Description string                     `json:"description,omitempty"`
	Method      string                     `json:"method"`
	Path        string                     `json:"path"`
	BaseURL     string                     `json:"base_url,omitempty"`
	Query       map[string]any             `json:"query,omitempty"`
	Body        any                        `json:"body,omitempty"`
	BodyType    string                     `json:"body_type,omitempty"`
	Headers     map[string]string          `json:"headers,omitempty"`
	Fields      []workflow.NodeSchemaField `json:"fields,omitempty"`
	Pagination  *Pagination                `json:"pagination,omitempty"`
	Output      *Output                    `json:"output,omitempty"`
	Binary      *Binary                    `json:"binary,omitempty"`
	N8nParamMap map[string]string          `json:"n8n_param_map,omitempty"`
	// Also are follow-up requests sent after the main one, in order, for
	// n8n operations that make several calls (e.g. update fields, then
	// tags). Each may read res (the main response). The node's output is
	// still the main response.
	Also []Step `json:"also,omitempty"`
	// Batch sends ONE request for all input items (n8n batchCreate/
	// batchUpdate/batchDelete): params come from the first item and the
	// items root holds every item's JSON.
	Batch bool `json:"batch,omitempty"`
	// Before is one request, or a list run in order, sent first. pre is
	// the last decoded response and pres.<i> each step's; later steps see
	// earlier ones (e.g. resolve a sheet title, then read its header row).
	Before Steps `json:"before,omitempty"`
}

// Step is one follow-up request of an operation. When is a template; an
// empty result skips the step (e.g. "{{ p.updateFields.tags }}").
type Step struct {
	When     string            `json:"when,omitempty"`
	Method   string            `json:"method"`
	Path     string            `json:"path"`
	BaseURL  string            `json:"base_url,omitempty"`
	Query    map[string]any    `json:"query,omitempty"`
	Body     any               `json:"body,omitempty"`
	BodyType string            `json:"body_type,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

// Key is "<resource>.<operation>", or just the operation when the package
// has no resources.
func (o *Operation) Key() string {
	if o.Resource == "" {
		return o.Operation
	}
	return o.Resource + "." + o.Operation
}

// Pagination types.
const (
	PageCursor     = "cursor"
	PageOffset     = "offset"
	PagePage       = "page"
	PageLinkHeader = "link_header"
	PageNextURL    = "next_url"
)

// Pagination describes how a list operation pages.
type Pagination struct {
	Type        string `json:"type"`
	CursorParam string `json:"cursor_param,omitempty"`
	CursorPath  string `json:"cursor_path,omitempty"`
	HasMorePath string `json:"has_more_path,omitempty"`
	OffsetParam string `json:"offset_param,omitempty"`
	LimitParam  string `json:"limit_param,omitempty"`
	PageParam   string `json:"page_param,omitempty"`
	NextURLPath string `json:"next_url_path,omitempty"`
	// NextURLPrefix (a template) is prepended to a relative next URL whose
	// base is not the request host (Confluence v2 _links.next).
	NextURLPrefix  string `json:"next_url_prefix,omitempty"`
	ResultsPath    string `json:"results_path,omitempty"`
	ReturnAllField string `json:"return_all_field,omitempty"`
	LimitField     string `json:"limit_field,omitempty"`

	// PageSize is the page size requested with limit_param (default 100).
	PageSize int `json:"page_size,omitempty"`
	// StartPage is the first page number for type "page" (default 1).
	StartPage *int `json:"start_page,omitempty"`
	// ParamIn is where page params go: "query" (default) or "body" (a JSON
	// body, e.g. search endpoints that take the cursor in the body).
	ParamIn string `json:"param_in,omitempty"`
	// MaxPages caps one run (default 1000); items get
	// pagination_truncated=true when it stops a list early.
	MaxPages int `json:"max_pages,omitempty"`
}

// Output says which part of the response becomes the node's items.
type Output struct {
	Path  string `json:"path,omitempty"`
	Split *bool  `json:"split,omitempty"`
	// HeaderRow turns a list of value rows into objects keyed by the
	// first row (Google Sheets values, CSV-like APIs).
	HeaderRow bool `json:"header_row,omitempty"`
	// Each replaces every result element by its value at this dot path
	// (Chargebee lists return [{"invoice": {…}}]; n8n emits the invoice).
	Each string `json:"each,omitempty"`
	// Match keeps only the result elements that satisfy n8n-style filter
	// rows (Google Sheets read lookups).
	Match *OutputMatch `json:"match,omitempty"`
	// Expand sends one request per result element (el = the element) and
	// replaces the element with the decoded response (Gmail lists ids,
	// then GETs each message).
	Expand *Step `json:"expand,omitempty"`
	// To reshapes each result element with a template (el = the element),
	// e.g. {"id": "{{ el.id }}", "$merge": "{{ el.values }}"}. ToWhen, a
	// template, skips the reshape when it renders empty/false (rawData).
	To     any    `json:"to,omitempty"`
	ToWhen string `json:"to_when,omitempty"`
}

// OutputMatch filters result elements by condition rows. Rows is a
// template resolving to a list of objects; each row's Column/Value keys
// name the element field and the wanted value. Combine ("AND"/"OR",
// default AND) and First (keep only the first match) are templates.
type OutputMatch struct {
	Rows    string `json:"rows"`
	Column  string `json:"column"`
	Value   string `json:"value"`
	Combine string `json:"combine,omitempty"`
	First   string `json:"first,omitempty"`
}

// Binary configures file upload and download.
type Binary struct {
	// Download: the response body is a file, stored on the output item's
	// binary data.
	Download bool `json:"download,omitempty"`
	// UploadField is the op param naming the input item's binary property
	// to send (n8n's "binaryPropertyName").
	UploadField string `json:"upload_field,omitempty"`
	// PropertyField is the op param naming the output binary property for a
	// download (default property "data").
	PropertyField string `json:"property_field,omitempty"`
	// MultipartField is the form field of the file part (default "file").
	MultipartField string `json:"multipart_field,omitempty"`
	// FileName and MimeType are templates for the uploaded part.
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// NodeTypeOf is the node type a package registers ("" for extends).
func (p *Package) NodeTypeOf() string {
	if p.Extends != "" {
		return ""
	}
	if p.NodeType != "" {
		return p.NodeType
	}
	return NodeTypePrefix + p.ID
}

// TargetType is the node type whose config runs this package: its own node
// type, or the native node it extends.
func (p *Package) TargetType() string {
	if p.Extends != "" {
		return p.Extends
	}
	return p.NodeTypeOf()
}

// Op returns the operation for (resource, operation).
func (p *Package) Op(resource, operation string) (*Operation, bool) {
	for i := range p.Operations {
		o := &p.Operations[i]
		if o.Resource == resource && o.Operation == operation {
			return o, true
		}
	}
	return nil, false
}

// DefaultResource is the resource used when a config names none: the first
// resource ("" for a package without resources).
func (p *Package) DefaultResource() string {
	if len(p.Resources) == 0 {
		return ""
	}
	return p.Resources[0].Value
}

// DefaultOperation is the first operation listed for resource.
func (p *Package) DefaultOperation(resource string) string {
	for _, o := range p.Operations {
		if o.Resource == resource {
			return o.Operation
		}
	}
	return ""
}

// PrimaryPlatform is the connection platform of the first credential, or "".
func (p *Package) PrimaryPlatform() string {
	for _, c := range p.Credentials {
		if pl := c.EffectivePlatform(); pl != "" {
			return pl
		}
	}
	return ""
}

// EffectivePlatform is reuse_platform when set, else platform.
func (c *Credential) EffectivePlatform() string {
	if c.ReusePlatform != "" {
		return c.ReusePlatform
	}
	return c.Platform
}

// hasMethod reports whether the credential offers method m.
func (c *Credential) hasMethod(m string) bool {
	for _, x := range c.Methods {
		if x == m {
			return true
		}
	}
	return false
}

// Steps is one Step or a list of them in JSON.
type Steps []Step

// UnmarshalJSON accepts a single step object or a list of steps.
func (s *Steps) UnmarshalJSON(b []byte) error {
	t := bytes.TrimSpace(b)
	if len(t) > 0 && t[0] == '{' {
		var one Step
		if err := json.Unmarshal(t, &one); err != nil {
			return err
		}
		*s = Steps{one}
		return nil
	}
	var many []Step
	if err := json.Unmarshal(t, &many); err != nil {
		return err
	}
	*s = many
	return nil
}
