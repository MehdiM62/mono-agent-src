package browserctl

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

// The origin policy (plan 2026-10-15-coder-browser, C2). An agent drives
// local and dev pages, plus the origins the user allowed; a human terminal
// is unrestricted. It is a courtesy layer against mistakes and page-borne
// prompt injection, not a sandbox: a full-access agent can read the bridge
// token and talk to the bridge itself (same stance as internal/orgsign).

// AllowlistSetting is the configs-table row holding the user's allowlist.
const AllowlistSetting = "browser_agent_allowlist"

// Policy decides which pages a caller may drive.
type Policy struct {
	// Agent: the caller is an agent (IsAgentContext). Nothing is refused
	// when false.
	Agent bool
	// Allowlist holds entries as NormalizeAllowEntry returns them:
	// "scheme://host[:port]" or "*.host".
	Allowlist []string
}

// NotAllowedError is a page an agent may not drive.
type NotAllowedError struct {
	Origin string
	Reason string
}

func (e *NotAllowedError) Error() string {
	msg := fmt.Sprintf("an agent may drive only local and dev pages and the origins the user allowed: %s is not allowed (%s)", e.Origin, e.Reason)
	if e.Allowable() {
		msg += fmt.Sprintf(" — the user can allow it with `monoagentcli browser allow add %s`", e.Origin)
	}
	return msg
}

// Allowable reports whether the user could put this origin on the allowlist.
func (e *NotAllowedError) Allowable() bool {
	return strings.HasPrefix(e.Origin, "http://") || strings.HasPrefix(e.Origin, "https://")
}

// Allowed reports whether the caller may drive a page at rawURL.
func (p Policy) Allowed(rawURL string) bool { return p.Check(rawURL) == nil }

// Check returns nil when the caller may drive a page at rawURL, else a
// *NotAllowedError naming the origin.
func (p Policy) Check(rawURL string) error {
	if !p.Agent {
		return nil
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
	if IsLocal(rawURL) {
		return nil
	}
	for _, e := range p.Allowlist {
		if entryMatches(e, u) {
			return nil
		}
	}
	return &NotAllowedError{Origin: origin, Reason: "not local and not on the allowlist"}
}

// SubmitGated reports whether a form submit on rawURL waits for a person's
// approval: an agent on an allowed origin that is not local.
func (p Policy) SubmitGated(rawURL string) bool {
	return p.Agent && p.Allowed(rawURL) && !IsLocal(rawURL)
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

// LoadAllowlist reads the user's allowlist; none stored is an empty list.
// Entries that no longer normalize are dropped.
func LoadAllowlist(db *sql.DB) ([]string, error) {
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

// SaveAllowlist stores entries (normalized, deduplicated, sorted).
func SaveAllowlist(db *sql.DB, entries []string) error {
	seen := map[string]bool{}
	doc := allowlistDoc{Origins: []string{}}
	for _, e := range entries {
		n, err := NormalizeAllowEntry(e)
		if err != nil {
			return err
		}
		if !seen[n] {
			seen[n] = true
			doc.Origins = append(doc.Origins, n)
		}
	}
	sort.Strings(doc.Origins)
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO configs (name, config_data, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET config_data = excluded.config_data, updated_at = excluded.updated_at`,
		AllowlistSetting, string(b))
	if err != nil {
		return fmt.Errorf("saving the browser allowlist: %w", err)
	}
	return nil
}
