package apipkg

import (
	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/workflow"
)

func init() {
	// Both hooks are lazy: nothing is loaded until a schema or platform is
	// looked up.
	workflow.RegisterSchemaProvider(schemaProvider)
	connections.AddPlatformSource(registerPlatforms)
}

// Register adds one node type per package that does not extend a native
// node. A type already in r is left alone.
func Register(r *workflow.NodeTypeRegistry) {
	for _, p := range packages() {
		t := p.NodeTypeOf()
		if t == "" || r.Has(t) {
			continue
		}
		pkg := p
		r.Register(t, func() workflow.NodeExecutor { return &Node{pkg: pkg} })
	}
}

// registerPlatforms registers each package credential's connection
// platform (the first package to name a platform defines it).
func registerPlatforms() {
	done := map[string]bool{}
	for _, p := range packages() {
		for i := range p.Credentials {
			c := &p.Credentials[i]
			if c.ReusePlatform != "" || done[c.Platform] {
				continue
			}
			def, ok := platformDef(p, c)
			if !ok {
				continue
			}
			if connections.RegisterPlatform(def) == nil {
				done[c.Platform] = true
			}
		}
	}
}

// platformDef converts a credential to a connections platform. A
// credential whose only method is "none" has no platform.
func platformDef(p *Package, c *Credential) (connections.PlatformDef, bool) {
	name := c.Name
	if name == "" {
		name = p.Title
	}
	def := connections.PlatformDef{
		ID: c.Platform, Name: name, Category: "app", ConnectVia: "API",
		Fields: map[connections.AuthMethod][]connections.CredentialField{}, IconEmoji: "🔌",
	}
	fields := make([]connections.CredentialField, 0, len(c.Fields))
	for _, f := range c.Fields {
		fields = append(fields, connections.CredentialField{
			Key: f.Key, Label: f.Label, Secret: f.Secret, Required: f.Required, HelpText: f.Help,
		})
	}
	add := func(m connections.AuthMethod) {
		for _, x := range def.Methods {
			if x == m {
				return
			}
		}
		def.Methods = append(def.Methods, m)
		if m != connections.MethodOAuth {
			def.Fields[m] = fields
		}
	}
	for _, m := range c.Methods {
		switch m {
		case "api_key", "bearer":
			add(connections.MethodAPIKey)
		case "basic":
			add(connections.MethodAppPass)
		case "oauth":
			if c.OAuth != nil {
				add(connections.MethodOAuth)
				def.OAuth = &connections.OAuthConfig{
					AuthURL: c.OAuth.AuthURL, TokenURL: c.OAuth.TokenURL,
					Scopes: c.OAuth.Scopes, ExtraParams: c.OAuth.ExtraParams,
				}
			}
		}
	}
	return def, len(def.Methods) > 0
}
