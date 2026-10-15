package browserctl

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// The origin policy (plan 2026-10-15-coder-browser, C2 and C8). An agent
// drives what its kind's level allows (permissions.go): local and dev pages
// at every level but off, other sites by level and by the user's site rules,
// a block rule winning over every level. A human terminal is unrestricted.
// It is a courtesy layer against mistakes and page-borne prompt injection,
// not a sandbox: a full-access agent can read the bridge token and talk to
// the bridge itself (same stance as internal/orgsign).

// AllowlistSetting is the configs-table row of the allowlist that came
// before the permissions; LoadPermissions reads it once as allow rules.
const AllowlistSetting = "browser_agent_allowlist"

// Policy decides which pages a caller may drive.
type Policy struct {
	// Agent: the caller is an agent. Nothing is refused when false.
	Agent bool
	// Kind is the agent's kind and Level its level ("" behaves as dev).
	Kind  string
	Level string
	// Allowlist and Blocklist hold the allow and block rules as
	// NormalizeAllowEntry returns them: "scheme://host[:port]" or "*.host".
	Allowlist []string
	Blocklist []string
}

func (p Policy) level() string {
	if p.Level == "" {
		return LevelDev
	}
	return p.Level
}

// NotAllowedError is a page an agent may not drive (dev level: not local
// and no allow rule; or not an http(s) page at all).
type NotAllowedError struct {
	Origin string
	Reason string
}

func (e *NotAllowedError) Error() string {
	msg := fmt.Sprintf("an agent may drive only local and dev pages and the origins the user allowed: %s is not allowed (%s)", e.Origin, e.Reason)
	if e.Allowable() {
		msg += fmt.Sprintf(" — the user can allow it with `monoagentcli browser allow add %s` or in Settings → Agent browser permissions", e.Origin)
	}
	return msg
}

// Allowable reports whether the user could put this origin on the allowlist.
func (e *NotAllowedError) Allowable() bool {
	return strings.HasPrefix(e.Origin, "http://") || strings.HasPrefix(e.Origin, "https://")
}

// BrowserOffError: the kind's level is off.
type BrowserOffError struct{ Kind string }

func (e *BrowserOffError) Error() string {
	return fmt.Sprintf("browser access is off for %s — Settings → Agent browser permissions", strings.ToLower(KindLabel(e.Kind)))
}

// SiteBlockedError: the user blocked the origin for every agent.
type SiteBlockedError struct{ Origin string }

func (e *SiteBlockedError) Error() string {
	return fmt.Sprintf("the user blocked %s for agents — Settings → Agent browser permissions", e.Origin)
}

// SiteNeedsApprovalError: at level ask, a site with no rule needs the
// user's yes first. Session turns it into a *SiteApprovalPendingError with
// the HIL item it raised (AskSite).
type SiteNeedsApprovalError struct {
	Origin string
	URL    string
	Kind   string
}

func (e *SiteNeedsApprovalError) Error() string {
	return fmt.Sprintf("%s is a new site for %s: the user has to allow it first", e.Origin, strings.ToLower(KindLabel(e.Kind)))
}

// Allowed reports whether the caller may drive a page at rawURL.
func (p Policy) Allowed(rawURL string) bool { return p.Check(rawURL) == nil }

// Check returns nil when the caller may drive a page at rawURL, else why
// not: *BrowserOffError, *SiteBlockedError, *NotAllowedError or
// *SiteNeedsApprovalError.
func (p Policy) Check(rawURL string) error {
	if !p.Agent {
		return nil
	}
	if p.level() == LevelOff {
		return &BrowserOffError{Kind: p.Kind}
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" {
		return &NotAllowedError{Origin: shortURL(rawURL), Reason: "not a URL"}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return &NotAllowedError{Origin: scheme + ":", Reason: "only http and https pages"}
	}
	origin := Origin(rawURL)
	if u.Hostname() == "" {
		return &NotAllowedError{Origin: origin, Reason: "no host"}
	}
	for _, e := range p.Blocklist {
		if entryMatches(e, u) {
			return &SiteBlockedError{Origin: origin}
		}
	}
	if IsLocal(rawURL) {
		return nil
	}
	switch p.level() {
	case LevelFull, LevelFullNoApprovals:
		return nil
	}
	for _, e := range p.Allowlist {
		if entryMatches(e, u) {
			return nil
		}
	}
	if p.level() == LevelAsk {
		return &SiteNeedsApprovalError{Origin: origin, URL: rawURL, Kind: p.Kind}
	}
	return &NotAllowedError{Origin: origin, Reason: "not local and not on the allowlist"}
}

// SubmitGated reports whether a form submit on rawURL waits for a person's
// approval: an agent on an allowed origin that is not local, unless its
// level is full-no-approvals.
func (p Policy) SubmitGated(rawURL string) bool {
	return p.Agent && p.level() != LevelFullNoApprovals && p.Allowed(rawURL) && !IsLocal(rawURL)
}

// IsLocal reports whether rawURL is an http(s) page on this machine or a
// dev host: localhost, *.localhost, 127.0.0.0/8, ::1, *.local, *.test, or
// a private (RFC 1918) IPv4 address.
func IsLocal(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || (ip.To4() != nil && ip.IsPrivate())
	}
	if host == "localhost" {
		return true
	}
	for _, suffix := range []string{".localhost", ".local", ".test"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// Origin is rawURL's scheme://host[:port] with a default port dropped, or a
// short form of rawURL when it has no host (about:blank, file:).
func Origin(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return shortURL(rawURL)
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" && port != defaultPort(scheme) {
		host += ":" + port
	}
	return scheme + "://" + host
}

func shortURL(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func defaultPort(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

// entryMatches reports whether one allowlist entry admits u: an origin
// entry matches that exact origin; "*.host" matches host and every
// subdomain of it over http or https on any port.
func entryMatches(entry string, u *url.URL) bool {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if rest, ok := strings.CutPrefix(entry, "*."); ok {
		return host == rest || strings.HasSuffix(host, "."+rest)
	}
	return entry == Origin(u.String())
}

// NormalizeAllowEntry checks one allowlist entry and returns its stored
// form: "scheme://host[:port]" (http or https, no path, the default port
// dropped) or "*.host" (at least two labels, so no whole top-level domain).
func NormalizeAllowEntry(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if rest, ok := strings.CutPrefix(s, "*."); ok {
		if !validHost(rest) || !strings.Contains(rest, ".") {
			return "", fmt.Errorf("%q: a wildcard entry is *.host with at least two labels, such as *.example.com", s)
		}
		return "*." + rest, nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("%q: an entry is scheme://host[:port] (http or https) or *.host", s)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q: an entry is an origin only, with no path, query or credentials", s)
	}
	return Origin(s), nil
}

func validHost(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

// allowlistDoc is the stored form of the setting.
type allowlistDoc struct {
	Origins []string `json:"origins"`
}

// LoadAllowlist is the allow rules of the permissions (LoadPermissions).
func LoadAllowlist(db *sql.DB) ([]string, error) {
	p, err := LoadPermissions(db)
	if err != nil {
		return nil, err
	}
	return p.Sited(RuleAllow), nil
}

// loadOldAllowlist reads the allowlist setting that came before the
// permissions; none stored is an empty list.
func loadOldAllowlist(db *sql.DB) ([]string, error) {
	var raw string
	err := db.QueryRow(`SELECT config_data FROM configs WHERE name = ?`, AllowlistSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the browser allowlist: %w", err)
	}
	var doc allowlistDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("reading the browser allowlist: %w", err)
	}
	out := []string{}
	for _, e := range doc.Origins {
		if n, err := NormalizeAllowEntry(e); err == nil {
			out = append(out, n)
		}
	}
	return out, nil
}

// SaveAllowlist makes entries the permissions' allow rules (block rules
// are kept; an entry that was blocked becomes allowed).
func SaveAllowlist(db *sql.DB, entries []string) error {
	p, err := LoadPermissions(db)
	if err != nil {
		return err
	}
	kept := []SiteRule{}
	for _, s := range p.Sites {
		if s.Rule == RuleBlock {
			kept = append(kept, s)
		}
	}
	p.Sites = kept
	for _, e := range entries {
		if _, _, err := p.SetSite(e, RuleAllow); err != nil {
			return err
		}
	}
	return SavePermissions(db, p)
}
