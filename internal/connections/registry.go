package connections

import "context"

// AuthMethod represents the authentication method for a platform.
type AuthMethod string

const (
	MethodOAuth   AuthMethod = "oauth"
	MethodAPIKey  AuthMethod = "apikey"
	MethodBrowser AuthMethod = "browser"
	MethodConnStr AuthMethod = "connstring"
	MethodAppPass AuthMethod = "apppassword"
	MethodSSHKey  AuthMethod = "sshkey"
)

// CredentialField describes a single credential input field.
type CredentialField struct {
	Key      string
	Label    string
	Secret   bool
	Required bool
	HelpURL  string
	HelpText string
}

// OAuthConfig holds OAuth 2.0 endpoint configuration.
type OAuthConfig struct {
	AuthURL      string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
	CallbackPort int
	ExtraParams  map[string]string // Additional auth URL query parameters (e.g., access_type=offline)
	// TokenFields are extra keys of the token response stored on the
	// connection at connect and refresh (GoTo: organizer_key, account_key).
	TokenFields []string
	// PlatformID identifies which registry entry this config came from, set
	// by callers when they copy *p.OAuth. It lets the token-exchange layer
	// (postForm) apply a provider's required client-auth style — most
	// providers are unaffected; see usesBasicClientAuth/postForm in oauth.go.
	PlatformID string
	// ConnectFields are asked before the browser flow (a tenant subdomain,
	// a Salesforce environment) and stored on the connection, where Tailor
	// reads them.
	ConnectFields []CredentialField
	// Tailor, when set, returns the config for a connection's data, at
	// connect and at every refresh: per-tenant auth/token URLs, scopes and
	// extra params (Zendesk subdomain, Microsoft tenant, Salesforce sandbox).
	Tailor func(cfg OAuthConfig, data map[string]interface{}) (OAuthConfig, error) `json:"-"`
	// AfterToken runs once a connect flow has a token and may add keys to
	// the connection's data (Mailchimp: the data centre from its metadata
	// endpoint).
	AfterToken func(ctx context.Context, data map[string]interface{}) error `json:"-"`
}

// PlatformDef defines a platform's connection capabilities.
type PlatformDef struct {
	ID         string
	Name       string
	Category   string
	ConnectVia string
	Methods    []AuthMethod
	Fields     map[AuthMethod][]CredentialField
	OAuth      *OAuthConfig
	OAuth1     *OAuth1Config // OAuth 1.0a (MethodOAuth1)
	IconEmoji  string
}

// sshKeyFields and sshPasswordFields are shared between "ssh" and "sftp" —
// both protocols authenticate identically over the same transport.
var sshKeyFields = []CredentialField{
	{Key: "host", Label: "Host", Secret: false, Required: true},
	{Key: "port", Label: "Port", Secret: false, Required: false, HelpText: "Defaults to 22"},
	{Key: "username", Label: "Username", Secret: false, Required: true},
	{Key: "private_key", Label: "Private Key (PEM)", Secret: true, Required: true},
	{Key: "passphrase", Label: "Key Passphrase", Secret: true, Required: false},
	{Key: "known_hosts", Label: "Known Hosts Path", Secret: false, Required: false, HelpText: "Path to a known_hosts file for host key verification (optional)"},
}

var sshPasswordFields = []CredentialField{
	{Key: "host", Label: "Host", Secret: false, Required: true},
	{Key: "port", Label: "Port", Secret: false, Required: false, HelpText: "Defaults to 22"},
	{Key: "username", Label: "Username", Secret: false, Required: true},
	{Key: "password", Label: "Password", Secret: true, Required: true},
}

// Registry is the map of all supported platforms keyed by platform ID.
// Its entries are declared in registryCore (registry_platforms.go) and
// registryMore (registry_platforms_more.go).
var Registry = mergePlatformDefs(registryCore, registryMore)

// mergePlatformDefs joins the platform tables into one map. A platform ID
// declared in two tables is a programming error (one map literal would not
// compile), so it panics during package initialization.
func mergePlatformDefs(tables ...map[string]PlatformDef) map[string]PlatformDef {
	out := make(map[string]PlatformDef)
	for _, t := range tables {
		for id, p := range t {
			if _, dup := out[id]; dup {
				panic("connections: platform " + id + " declared twice")
			}
			out[id] = p
		}
	}
	return out
}

// Get returns the PlatformDef for the given ID and a boolean indicating existence.
func Get(id string) (PlatformDef, bool) {
	if p, ok := Registry[id]; ok {
		return p, ok
	}
	return dynamicGet(id)
}

// All returns all platform definitions as a slice.
func All() []PlatformDef {
	out := make([]PlatformDef, 0, len(Registry))
	for _, p := range Registry {
		out = append(out, p)
	}
	return append(out, dynamicAll()...)
}

// ByConnectVia returns all platforms with the given ConnectVia value.
func ByConnectVia(via string) []PlatformDef {
	var out []PlatformDef
	for _, p := range All() {
		if p.ConnectVia == via {
			out = append(out, p)
		}
	}
	return out
}
