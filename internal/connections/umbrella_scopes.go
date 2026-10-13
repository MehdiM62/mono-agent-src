package connections

import (
	"fmt"
	"strings"
)

// ResolveScopes turns scope-group names into the OAuth scopes an umbrella
// platform requests. No groups selects every group that is not opt-in (the
// default set); "all" selects every group, opt-in ones included. The
// platform's always-on scopes are included, duplicates dropped, order kept.
// An unknown group name is an error that lists the valid ones.
func ResolveScopes(platform string, groups []string) ([]string, error) {
	u, ok := umbrellas[platform]
	if !ok {
		return nil, fmt.Errorf("platform %q has no scope groups (only %s do)", platform, strings.Join(umbrellaOrder, ", "))
	}
	selected, err := normalizeGroupNames(platform, groups)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	add := func(scopes []string) {
		for _, s := range scopes {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	add(u.Always)
	for _, g := range u.Groups {
		if selected[g.Name] {
			add(g.Scopes)
		}
	}
	return out, nil
}

func mustResolveScopes(platform string, groups []string) []string {
	s, err := ResolveScopes(platform, groups)
	if err != nil {
		panic(err)
	}
	return s
}

// normalizeGroupNames validates group names (case-insensitive, trimmed,
// empty entries ignored) and returns the selected set; an empty selection
// means every group that is not opt-in.
func normalizeGroupNames(platform string, groups []string) (map[string]bool, error) {
	u := umbrellas[platform]
	valid := map[string]bool{}
	var names []string
	for _, g := range u.Groups {
		valid[g.Name] = true
		names = append(names, g.Name)
	}
	selected := map[string]bool{}
	for _, raw := range groups {
		g := strings.ToLower(strings.TrimSpace(raw))
		if g == "" {
			continue
		}
		if g == "all" {
			for n := range valid {
				selected[n] = true
			}
			continue
		}
		if !valid[g] {
			return nil, fmt.Errorf("unknown %s scope group %q (valid: %s, all)", platform, raw, strings.Join(names, ", "))
		}
		selected[g] = true
	}
	if len(selected) == 0 {
		for _, g := range u.Groups {
			if !g.OptIn {
				selected[g.Name] = true
			}
		}
	}
	return selected, nil
}

// GrantedGroups returns, in catalogue order, the scope groups of an umbrella
// platform fully covered by a granted scope string (space- or
// comma-separated, as token endpoints return it).
func GrantedGroups(platform, granted string) []string {
	u, ok := umbrellas[platform]
	if !ok {
		return nil
	}
	have := splitScopes(granted)
	var out []string
	for _, g := range u.Groups {
		if len(missingScopes(platform, have, g.Scopes)) == 0 {
			out = append(out, g.Name)
		}
	}
	return out
}

// ScopeError reports that an umbrella connection lacks scopes a node needs,
// with the exact command that adds them.
type ScopeError struct {
	Platform string   // umbrella platform: google | microsoft
	Group    string   // the scope group the node needs
	Missing  []string // scopes absent from the stored token
	Command  string   // the connect command that re-consents with the group added
}

func (e *ScopeError) Error() string {
	return fmt.Sprintf("the %s connection was not granted the %q scope group (missing %s); add it with: %s",
		e.Platform, e.Group, strings.Join(e.Missing, ", "), e.Command)
}

// ReconnectCommand is the `connect` command that re-consents an umbrella
// connection for its currently granted groups plus extra.
func ReconnectCommand(platform, granted string, extra ...string) string {
	set := map[string]bool{}
	for _, g := range GrantedGroups(platform, granted) {
		set[g] = true
	}
	for _, g := range extra {
		set[g] = true
	}
	var groups []string
	for _, g := range umbrellas[platform].Groups {
		if set[g.Name] {
			groups = append(groups, g.Name)
		}
	}
	return fmt.Sprintf("monoagentcli connect %s --scopes %s", platform, strings.Join(groups, ","))
}

// productGroup maps a node's credential platform (a legacy per-product
// platform such as "gmail", or a scope-group name such as "calendar") to the
// umbrella scope group it needs on the given umbrella.
func productGroup(umbrella, product string) (string, bool) {
	u, ok := umbrellas[umbrella]
	if !ok {
		return "", false
	}
	for _, m := range u.Members {
		if m.Platform == product {
			return m.Group, true
		}
	}
	for _, g := range u.Groups {
		if g.Name == product {
			return g.Name, true
		}
	}
	return "", false
}

// CheckCredentialScopes verifies that an injected credential (the node's
// config["credential"]) carries the scopes the node's product needs. It only
// judges umbrella connections — those record "oauth_platform" — and only
// when the token's granted scopes are known; per-product connections were
// consented for exactly their product and pass unchanged. product is the
// node's credential platform (gmail, google_sheets, outlook, …) or an
// umbrella scope-group name (calendar, docs, teams, …).
func CheckCredentialScopes(cred map[string]interface{}, product string) error {
	umbrella := getStr(cred, "oauth_platform")
	if !IsUmbrella(umbrella) {
		return nil
	}
	granted := getStr(cred, "scope")
	if strings.TrimSpace(granted) == "" {
		return nil
	}
	group, ok := productGroup(umbrella, product)
	if !ok {
		return nil
	}
	var need []string
	for _, g := range umbrellas[umbrella].Groups {
		if g.Name == group {
			need = g.Scopes
		}
	}
	missing := missingScopes(umbrella, splitScopes(granted), need)
	if len(missing) == 0 {
		return nil
	}
	return &ScopeError{
		Platform: umbrella,
		Group:    group,
		Missing:  missing,
		Command:  ReconnectCommand(umbrella, granted, group),
	}
}

func splitScopes(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' || r == '\t' })
}

// missingScopes returns the required scopes not covered by any granted one.
func missingScopes(platform string, granted, required []string) []string {
	var missing []string
	for _, r := range required {
		covered := false
		for _, g := range granted {
			if scopeCovers(platform, g, r) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, r)
		}
	}
	return missing
}

// normScope lowercases a scope and strips the provider's URL prefix, so
// "https://graph.microsoft.com/Mail.Send" and "Mail.Send" compare equal.
func normScope(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, strings.ToLower(googleScopePrefix))
	s = strings.TrimPrefix(s, strings.ToLower(microsoftScopePrefix))
	return s
}

// googleImplied lists Google scopes that grant narrower ones beyond the
// generic "x covers x.readonly" rule.
var googleImplied = map[string][]string{
	"drive":             {"drive.file", "drive.metadata", "drive.metadata.readonly"},
	"gmail.modify":      {"gmail.readonly", "gmail.send", "gmail.compose", "gmail.labels", "gmail.insert"},
	"calendar":          {"calendar.events", "calendar.events.readonly", "calendar.readonly"},
	"forms.body":        {"forms.body.readonly"},
	"youtube.force-ssl": {"youtube", "youtube.readonly"},
	"youtube":           {"youtube.readonly"},
}

// scopeCovers reports whether granted scope g satisfies required scope r.
func scopeCovers(platform, g, r string) bool {
	g, r = normScope(g), normScope(r)
	if g == r {
		return true
	}
	switch platform {
	case "google":
		if g == "https://mail.google.com/" && strings.HasPrefix(r, "gmail.") {
			return true
		}
		if g+".readonly" == r {
			return true
		}
		for _, implied := range googleImplied[g] {
			if implied == r {
				return true
			}
		}
	case "microsoft":
		// Foo.ReadWrite[.All] covers Foo.Read[.All]; Foo.X.All covers Foo.X.
		gAll, rAll := strings.HasSuffix(g, ".all"), strings.HasSuffix(r, ".all")
		if rAll && !gAll {
			return false
		}
		gb, rb := strings.TrimSuffix(g, ".all"), strings.TrimSuffix(r, ".all")
		if gb == rb {
			return true
		}
		if strings.HasSuffix(rb, ".read") && gb == strings.TrimSuffix(rb, ".read")+".readwrite" {
			return true
		}
	}
	return false
}
