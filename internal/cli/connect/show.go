package connect

import (
	"fmt"
	"io"
	"strings"

	"github.com/monoes/mono-agent/internal/connections"
	"github.com/spf13/cobra"
)

// platformField is one credential field of `connect show --json`.
type platformField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Secret   bool   `json:"secret,omitempty"`
	Required bool   `json:"required,omitempty"`
	HelpText string `json:"help_text,omitempty"`
	HelpURL  string `json:"help_url,omitempty"`
}

// platformOAuth is a platform's OAuth 2.0 flow in `connect show --json`.
type platformOAuth struct {
	AuthURL  string   `json:"auth_url"`
	TokenURL string   `json:"token_url"`
	Scopes   []string `json:"scopes,omitempty"`
	// ConnectFields are the --field values `connect oauth` takes.
	ConnectFields []platformField `json:"connect_fields,omitempty"`
	// PerTenant: the endpoints depend on the connect fields.
	PerTenant bool `json:"per_tenant,omitempty"`
}

// platformOAuth1 is a platform's OAuth 1.0a flow in `connect show --json`.
// Empty endpoints come from the connection's own fields (generic_oauth1).
type platformOAuth1 struct {
	RequestTokenURL string `json:"request_token_url,omitempty"`
	AuthURL         string `json:"authorization_url,omitempty"`
	AccessTokenURL  string `json:"access_token_url,omitempty"`
	SignatureMethod string `json:"signature_method,omitempty"`
}

// platformView is `connect show --json`: how a platform connects.
type platformView struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Category   string                     `json:"category"`
	ConnectVia string                     `json:"connect_via"`
	Methods    []string                   `json:"methods"`
	Fields     map[string][]platformField `json:"fields,omitempty"`
	OAuth      *platformOAuth             `json:"oauth,omitempty"`
	OAuth1     *platformOAuth1            `json:"oauth1,omitempty"`
	Connect    []string                   `json:"connect"`
}

func toPlatformFields(fields []connections.CredentialField) []platformField {
	out := make([]platformField, len(fields))
	for i, f := range fields {
		out[i] = platformField{Key: f.Key, Label: f.Label, Secret: f.Secret, Required: f.Required, HelpText: f.HelpText, HelpURL: f.HelpURL}
	}
	return out
}

func newPlatformView(p connections.PlatformDef) platformView {
	v := platformView{ID: p.ID, Name: p.Name, Category: p.Category, ConnectVia: p.ConnectVia, Fields: map[string][]platformField{}}
	for _, m := range p.Methods {
		v.Methods = append(v.Methods, string(m))
		if fs := p.Fields[m]; len(fs) > 0 {
			v.Fields[string(m)] = toPlatformFields(fs)
		}
	}
	if p.OAuth != nil {
		v.OAuth = &platformOAuth{AuthURL: p.OAuth.AuthURL, TokenURL: p.OAuth.TokenURL, Scopes: p.OAuth.Scopes,
			ConnectFields: toPlatformFields(p.OAuth.ConnectFields), PerTenant: p.OAuth.Tailor != nil}
	}
	if p.OAuth1 != nil && hasMethod(p, connections.MethodOAuth1) {
		v.OAuth1 = &platformOAuth1{RequestTokenURL: p.OAuth1.RequestTokenURL, AuthURL: p.OAuth1.AuthURL,
			AccessTokenURL: p.OAuth1.AccessTokenURL, SignatureMethod: p.OAuth1.SignatureMethod}
		if v.OAuth1.SignatureMethod == "" && v.OAuth1.RequestTokenURL != "" {
			v.OAuth1.SignatureMethod = "HMAC-SHA1"
		}
	}
	v.Connect = connectCommands(p)
	return v
}

// connectCommands are the commands that connect p, with its --field keys.
func connectCommands(p connections.PlatformDef) []string {
	fieldArgs := func(fs []connections.CredentialField) string {
		var b strings.Builder
		for _, f := range fs {
			if f.Required && !f.Secret {
				fmt.Fprintf(&b, " --field %s=…", f.Key)
			}
		}
		return b.String()
	}
	var out []string
	switch {
	case p.OAuth != nil:
		out = append(out, "monoagentcli connect oauth "+p.ID+fieldArgs(p.OAuth.ConnectFields))
	case p.OAuth1 != nil && hasMethod(p, connections.MethodOAuth1):
		out = append(out, "monoagentcli connect oauth "+p.ID+fieldArgs(p.Fields[connections.MethodOAuth1]))
	}
	for _, m := range p.Methods {
		switch m {
		case connections.MethodOAuth, connections.MethodOAuth1:
		case connections.MethodBrowser:
			out = append(out, "monoagentcli login "+p.ID)
		default:
			out = append(out, fmt.Sprintf("monoagentcli connect save %s --method %s --stdin-json", p.ID, m))
		}
	}
	return out
}

// newConnectShowCmd shows how one platform connects: its methods, each
// method's fields, its OAuth 2.0 connect fields or OAuth 1.0a endpoints,
// and the commands that connect it.
func (d Deps) newConnectShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <platform>",
		Short: "Show how a platform connects: methods, fields, OAuth connect fields and commands",
		Long: "Prints a supported platform's auth methods, the fields of each, its OAuth 2.0 endpoints and " +
			"connect fields (the --field values of `connect oauth`) or its OAuth 1.0a endpoints, and the " +
			"commands that connect it. Exit code 3 for an unknown platform.",
		Example: "  monoagentcli connect show salesforce\n  monoagentcli connect show generic_oauth1 --json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, ok := connections.Get(args[0])
			if !ok {
				return d.InvalidInput("unknown platform %q (see `monoagentcli connect list --all`)", args[0])
			}
			v := newPlatformView(p)
			if d.JSONOutput() {
				return d.WriteJSON(cmd.OutOrStdout(), v)
			}
			printPlatformView(cmd.OutOrStdout(), v)
			return nil
		},
	}
}

func printPlatformView(out io.Writer, v platformView) {
	fmt.Fprintf(out, "%s (%s) — %s, connect via %s\n", v.Name, v.ID, v.Category, v.ConnectVia)
	fmt.Fprintf(out, "Methods: %s\n", strings.Join(v.Methods, ", "))
	printFields := func(title string, fs []platformField) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(out, "\n%s\n", title)
		for _, f := range fs {
			var tags []string
			if f.Required {
				tags = append(tags, "required")
			}
			if f.Secret {
				tags = append(tags, "secret")
			}
			line := fmt.Sprintf("  %-20s %s", f.Key, f.Label)
			if len(tags) > 0 {
				line += " (" + strings.Join(tags, ", ") + ")"
			}
			if f.HelpText != "" {
				line += " — " + f.HelpText
			}
			fmt.Fprintln(out, line)
		}
	}
	for _, m := range v.Methods {
		printFields("Fields ("+m+"):", v.Fields[m])
	}
	if o := v.OAuth; o != nil {
		fmt.Fprintf(out, "\nOAuth 2.0: %s\n", o.AuthURL)
		if o.PerTenant {
			fmt.Fprintln(out, "  endpoints follow the connect fields")
		}
		printFields("OAuth connect fields (connect oauth --field key=value):", o.ConnectFields)
	}
	if o := v.OAuth1; o != nil {
		if o.RequestTokenURL == "" {
			fmt.Fprintln(out, "\nOAuth 1.0a:")
			fmt.Fprintln(out, "  endpoints and signature method are the connection's request_token_url, authorization_url, access_token_url and signature_method fields")
		} else {
			fmt.Fprintf(out, "\nOAuth 1.0a (%s):\n", o.SignatureMethod)
			fmt.Fprintf(out, "  request token  %s\n  authorize      %s\n  access token   %s\n", o.RequestTokenURL, o.AuthURL, o.AccessTokenURL)
		}
	}
	fmt.Fprintln(out, "\nConnect with:")
	for _, c := range v.Connect {
		fmt.Fprintf(out, "  %s\n", c)
	}
}
