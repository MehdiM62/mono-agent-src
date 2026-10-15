package browserctl

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Browser permissions (plan C8): a level per kind of agent, and per-site
// allow/block rules for all kinds, set by the user in Settings or with
// `browser permissions`. A human terminal is never restricted.

// PermissionsSetting is the configs-table row holding the permissions.
const PermissionsSetting = "browser_agent_permissions"

// Kinds of agent.
const (
	KindCoder = "coder" // coder chats and their dynorg workers
	KindOrg   = "org"   // monomind org roles, org triggers
	KindChat  = "chat"  // the in-app chat and the workflow runs it starts
	KindOther = "other" // any other agent shell (Claude Code, Codex, … in a terminal)
)

// Kinds lists every kind, in the order the GUI shows them.
var Kinds = []string{KindCoder, KindOrg, KindChat, KindOther}

// Levels, most to least access.
const (
	LevelFullNoApprovals = "full-no-approvals"
	LevelFull            = "full"
	LevelDev             = "dev"
	LevelAsk             = "ask"
	LevelOff             = "off"
)

// LevelInfo is one level as Settings shows it.
type LevelInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Catalog lists the levels with the label and one-line description the
// GUI shows verbatim.
func Catalog() []LevelInfo {
	return []LevelInfo{
		{LevelFullNoApprovals, "Full access, no approvals", "Any site; form submits go through without asking you, and scripts run on any page, where they can read that site's cookies and storage."},
		{LevelFull, "Full access", "Any site; a form submit on a real site waits for your approval."},
		{LevelDev, "Dev pages + allowed sites", "Localhost and dev pages, plus sites you allow; submits on real sites wait for your approval."},
		{LevelAsk, "Ask for new sites", "Like dev pages + allowed sites, but a new site asks you first instead of being refused."},
		{LevelOff, "Off", "This kind of agent can't use the browser."},
	}
}

// KindLabel is a kind's name in Settings and in messages.
func KindLabel(kind string) string {
	switch kind {
	case KindCoder:
		return "Coder chats"
	case KindOrg:
		return "Org roles"
	case KindChat:
		return "In-app chat"
	case KindOther:
		return "Other agents (terminal sessions)"
	}
	return kind
}

// DefaultLevels are the levels of kinds the user did not set: dev pages
// for every agent shell, as shipped in v0.141.0; the in-app chat keeps any
// site behind submit approvals, as it had.
var DefaultLevels = map[string]string{KindCoder: LevelDev, KindOrg: LevelDev, KindChat: LevelFull, KindOther: LevelDev}

// Site rules.
const (
	RuleAllow = "allow"
	RuleBlock = "block"
)

// SiteRule is one site's rule; Origin is in its canonical stored form
// (scheme://host[:port] or *.host, and for a block also a bare host). An
// allow admits exactly that origin (*.host: the host and its subdomains);
// a block covers the host and its subdomains on every scheme and port.
type SiteRule struct {
	Origin string `json:"origin"`
	Rule   string `json:"rule"`
}

// Permissions is the stored setting.
type Permissions struct {
	Levels map[string]string `json:"levels"`
	Sites  []SiteRule        `json:"sites"`
}

// DefaultPermissions has every kind at its default level and no site rules.
func DefaultPermissions() Permissions {
	p := Permissions{Levels: map[string]string{}, Sites: []SiteRule{}}
	for k, v := range DefaultLevels {
		p.Levels[k] = v
	}
	return p
}

func validLevel(level string) bool {
	for _, l := range Catalog() {
		if l.ID == level {
			return true
		}
	}
	return false
}

func validKind(kind string) bool {
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// LoadPermissions reads the setting; kinds it does not name have their
// default level. With no setting yet, the old allowlist (browser_agent_
// allowlist) is read as allow rules; the first SavePermissions keeps them.
func LoadPermissions(db *sql.DB) (Permissions, error) {
	p := DefaultPermissions()
	var raw string
	err := db.QueryRow(`SELECT config_data FROM configs WHERE name = ?`, PermissionsSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		old, err := loadOldAllowlist(db)
		if err != nil {
			return p, err
		}
		for _, o := range old {
			p.Sites = append(p.Sites, SiteRule{Origin: o, Rule: RuleAllow})
		}
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("reading the browser permissions: %w", err)
	}
	var stored Permissions
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return p, fmt.Errorf("reading the browser permissions: %w", err)
	}
	for k, v := range stored.Levels {
		if validKind(k) && validLevel(v) {
			p.Levels[k] = v
		}
	}
	for _, s := range stored.Sites {
		if c, err := canonSite(s); err == nil {
			p.Sites = mergeSite(p.Sites, c)
		}
	}
	return p, nil
}

// canonSite checks one rule and puts its origin in canonical form.
func canonSite(s SiteRule) (SiteRule, error) {
	if s.Rule != RuleAllow && s.Rule != RuleBlock {
		return s, fmt.Errorf("%s: a site rule is allow or block, not %q", s.Origin, s.Rule)
	}
	o, err := normalizeSiteEntry(s.Origin, s.Rule)
	if err != nil {
		return s, err
	}
	return SiteRule{Origin: o, Rule: s.Rule}, nil
}

// mergeSite adds s to sites, one rule per origin: a block wins over an
// allow stored under the same canonical origin.
func mergeSite(sites []SiteRule, s SiteRule) []SiteRule {
	for i, have := range sites {
		if have.Origin == s.Origin {
			if s.Rule == RuleBlock {
				sites[i] = s
			}
			return sites
		}
	}
	return append(sites, s)
}

// SavePermissions stores p (sites sorted, one rule per origin).
func SavePermissions(db *sql.DB, p Permissions) error {
	out := DefaultPermissions()
	for k, v := range p.Levels {
		if !validKind(k) || !validLevel(v) {
			return fmt.Errorf("%s: %s is not a level for %s", PermissionsSetting, v, k)
		}
		out.Levels[k] = v
	}
	for _, s := range p.Sites {
		c, err := canonSite(s)
		if err != nil {
			return err
		}
		out.Sites = mergeSite(out.Sites, c)
	}
	sort.Slice(out.Sites, func(i, j int) bool { return out.Sites[i].Origin < out.Sites[j].Origin })
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO configs (name, config_data, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET config_data = excluded.config_data, updated_at = excluded.updated_at`,
		PermissionsSetting, string(b))
	if err != nil {
		return fmt.Errorf("saving the browser permissions: %w", err)
	}
	return nil
}

// Level is kind's level.
func (p Permissions) Level(kind string) string {
	if l, ok := p.Levels[kind]; ok {
		return l
	}
	return DefaultLevels[kind]
}

// SetLevel sets kind's level.
func (p *Permissions) SetLevel(kind, level string) error {
	if !validKind(kind) {
		return fmt.Errorf("%q is not a kind of agent: %s", kind, strings.Join(Kinds, ", "))
	}
	if !validLevel(level) {
		ids := []string{}
		for _, l := range Catalog() {
			ids = append(ids, l.ID)
		}
		return fmt.Errorf("%q is not a level: %s", level, strings.Join(ids, ", "))
	}
	if p.Levels == nil {
		p.Levels = map[string]string{}
	}
	p.Levels[kind] = level
	return nil
}

// SetSite sets origin's rule (allow or block), or with "clear" removes it,
// and returns the origin as stored (canonical). found reports whether it
// had a rule.
func (p *Permissions) SetSite(origin, rule string) (stored string, found bool, err error) {
	if rule != RuleAllow && rule != RuleBlock && rule != "clear" {
		return "", false, fmt.Errorf("a site rule is allow, block or clear, not %q", rule)
	}
	o, err := normalizeSiteEntry(origin, rule)
	if err != nil {
		return "", false, err
	}
	kept := []SiteRule{}
	for _, s := range p.Sites {
		if s.Origin == o {
			found = true
			continue
		}
		kept = append(kept, s)
	}
	if rule != "clear" {
		kept = append(kept, SiteRule{Origin: o, Rule: rule})
	}
	p.Sites = kept
	return o, found, nil
}

// Rule is origin's stored rule ("" for none).
func (p Permissions) Rule(origin string) string {
	o, err := normalizeSiteEntry(origin, RuleBlock)
	if err != nil {
		return ""
	}
	for _, s := range p.Sites {
		if s.Origin == o {
			return s.Rule
		}
	}
	return ""
}

// Sited lists the origins with rule (allow or block).
func (p Permissions) Sited(rule string) []string {
	out := []string{}
	for _, s := range p.Sites {
		if s.Rule == rule {
			out = append(out, s.Origin)
		}
	}
	return out
}

// PolicyFor is the policy for an agent of kind ("" is a human terminal:
// unrestricted).
func (p Permissions) PolicyFor(kind string) Policy {
	if kind == "" {
		return Policy{}
	}
	return Policy{Agent: true, Kind: kind, Level: p.Level(kind), Allowlist: p.Sited(RuleAllow), Blocklist: p.Sited(RuleBlock)}
}

// Environment that tells kinds of agent apart.
const (
	EnvAgentKind = "MONOAGENT_AGENT_KIND" // "coder" (coder turns, dynorg workers) or "org"
	EnvAICaller  = "MONOAGENT_AI_CALLER"  // "chat": a run the in-app chat started
)

// AgentKind is the kind of agent running this process, "" for a human
// terminal. An agent shell (IsAgentContext) is the kind it declares —
// coder with MONOAGENT_AGENT_KIND=coder, org with MONOAGENT_AGENT_KIND=org
// or MONOMIND_ORG_ROLE — when a token its spawner issued vouches for it
// (IssueAgentKind), else other: a shell is never chat. chat is a process
// with MONOAGENT_AI_CALLER=chat and no shell markers.
func AgentKind() string {
	if IsAgentContext() {
		if kind := declaredKind(); kind != "" && kindVouched(kind) {
			return kind
		}
		return KindOther
	}
	if strings.TrimSpace(os.Getenv(EnvAICaller)) == KindChat {
		return KindChat
	}
	return ""
}

// declaredKind is the kind an agent shell's environment claims.
func declaredKind() string {
	switch kind := strings.TrimSpace(os.Getenv(EnvAgentKind)); {
	case kind == KindCoder || kind == KindOrg:
		return kind
	case strings.TrimSpace(os.Getenv("MONOMIND_ORG_ROLE")) != "":
		return KindOrg
	}
	return ""
}
