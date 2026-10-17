package browserctl

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/testdb"
)

// policyWith is kind other at level with these rules ("origin=allow|block").
func policyWith(t *testing.T, level string, rules ...string) Policy {
	t.Helper()
	p := DefaultPermissions()
	if err := p.SetLevel(KindOther, level); err != nil {
		t.Fatal(err)
	}
	for _, r := range rules {
		origin, rule := r[:len(r)-len("=allow")], "allow"
		if r[len(r)-len("block"):] == "block" {
			origin, rule = r[:len(r)-len("=block")], RuleBlock
		}
		if _, _, err := p.SetSite(origin, rule); err != nil {
			t.Fatalf("%s: %v", r, err)
		}
	}
	return p.PolicyFor(KindOther)
}

// A block on https://bank.example holds however the agent spells the
// host: a trailing dot, a fullwidth dot, upper case, another scheme or
// port, a subdomain, or the other IDN form; even at full-no-approvals.
func TestBlockRulesFailClosed(t *testing.T) {
	pol := policyWith(t, LevelFullNoApprovals, "https://bank.example=block", "https://bücher.example=block",
		"https://xn--brse-5qa.example=block", "http://10.1.2.3=block", "files.example=block")
	for _, u := range []string{
		"https://bank.example/", "https://bank.example./login", "https://bank\uff0eexample/", "https://BANK.Example/",
		"http://bank.example/", "https://bank.example:8443/", "https://www.bank.example/", "https://a.b.bank.example/",
		"https://xn--bcher-kva.example/", "https://Bücher.example/", "https://börse.example/",
		"http://167838211/", "http://0xa.1.2.3/", "http://10.1.515/",
		"https://files.example/", "http://cdn.files.example:8080/",
	} {
		var be *SiteBlockedError
		if err := pol.Check(u); !errors.As(err, &be) {
			t.Errorf("%s: %v, want blocked", u, err)
		}
	}
	for _, u := range []string{"https://notbank.example/", "https://bank.example.org/", "https://bankexample/"} {
		if err := pol.Check(u); err != nil {
			t.Errorf("%s: %v, want allowed at full-no-approvals", u, err)
		}
	}
	// A host the browser could read differently from us matches no rule:
	// it is refused rather than let past a block.
	for _, u := range []string{"https://bank\u2024example/", "http://10.1.2.3.4/", "http://256.1.2.3/"} {
		var na *NotAllowedError
		if err := pol.Check(u); !errors.As(err, &na) {
			t.Errorf("%s: %v, want refused", u, err)
		}
	}
}

// An allow stays exact: scheme, host and port; *.host for subdomains.
func TestAllowRulesStayExact(t *testing.T) {
	pol := policyWith(t, LevelDev, "https://shop.example=allow", "*.corp.example=allow", "https://bücher.example=allow")
	cases := map[string]bool{
		"https://shop.example/":          true,
		"https://SHOP.example./cart":     true,
		"https://shop\uff0eexample/":     true,
		"http://shop.example/":           false,
		"https://shop.example:8443/":     false,
		"https://www.shop.example/":      false,
		"https://corp.example/":          true,
		"http://a.corp.example:8080/":    true,
		"https://xn--bcher-kva.example/": true,
		"http://bücher.example/":         false,
	}
	for u, want := range cases {
		if got := pol.Check(u) == nil; got != want {
			t.Errorf("%s: allowed=%v, want %v", u, got, want)
		}
	}
}

// Rules are stored canonical, and read back canonical: two spellings of
// one origin are one rule, a block winning.
func TestSiteRulesAreStoredCanonical(t *testing.T) {
	p := DefaultPermissions()
	for _, c := range []struct{ in, rule, want string }{
		{"HTTPS://Bücher.Example:443/", RuleAllow, "https://xn--bcher-kva.example"},
		{"https://bank.example.", RuleBlock, "https://bank.example"},
		{"Files.Example", RuleBlock, "files.example"},
		{"*.Corp\uff0eExample", RuleAllow, "*.corp.example"},
	} {
		got, _, err := p.SetSite(c.in, c.rule)
		if err != nil || got != c.want {
			t.Errorf("%s: stored %q (%v), want %q", c.in, got, err, c.want)
		}
	}
	if _, _, err := p.SetSite("files.example", RuleAllow); err == nil {
		t.Error("an allow on a bare host was taken: an allow is an exact origin")
	}
	if _, found, err := p.SetSite("https://Bank.Example", "clear"); err != nil || !found {
		t.Errorf("clear by another spelling: found=%v %v", found, err)
	}

	db := testdb.Open(t).DB
	if _, err := db.Exec(`INSERT INTO configs (name, config_data) VALUES (?, ?)`, PermissionsSetting,
		`{"levels":{},"sites":[{"origin":"https://Shop.Example.","rule":"allow"},{"origin":"https://shop.example:443","rule":"block"}]}`); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPermissions(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sites) != 1 || got.Sites[0] != (SiteRule{Origin: "https://shop.example", Rule: RuleBlock}) {
		t.Fatalf("read back: %+v", got.Sites)
	}
}

// Origin is the browser's: the xn-- host, no default port, a trailing dot
// kept (the extension compares it with `new URL(u).origin`).
func TestOriginIsTheBrowsers(t *testing.T) {
	for in, want := range map[string]string{
		"https://Bücher.example:443/x": "https://xn--bcher-kva.example",
		"https://bank.example./":       "https://bank.example.",
		"http://0x7f.1:8080/":          "http://127.0.0.1:8080",
		"https://bank\uff0eexample/":   "https://bank.example",
	} {
		if got := Origin(in); got != want {
			t.Errorf("Origin(%q) = %q, want %q", in, got, want)
		}
	}
	if got := UnicodeHost("xn--bcher-kva.example"); got != "bücher.example" {
		t.Errorf("UnicodeHost: %q", got)
	}
}

// A HIL card for an internationalized host carries its xn-- form next to
// the Unicode one, however the agent spelled it; a plain host carries
// neither.
func TestItemsCarryTheASCIIHost(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t).DB
	card := func(id string) (map[string]any, string) {
		var ro, name string
		if err := db.QueryRow(`SELECT readonly_data, node_name FROM hil_pending WHERE id = ?`, id).Scan(&ro, &name); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(ro), &m)
		return m, name
	}
	for _, u := range []string{"https://bücher.example/", "https://xn--bcher-kva.example:8443/"} {
		id, err := AskSite(ctx, db, "p1", KindOther, u, nil)
		if err != nil {
			t.Fatal(err)
		}
		m, name := card(id)
		if m["asciiHost"] != "xn--bcher-kva.example" || m["unicodeHost"] != "bücher.example" || !strings.Contains(name, "(xn--bcher-kva.example)") {
			t.Errorf("site item for %s: %v %q", u, m, name)
		}
	}
	it, err := CreateSubmitItem(ctx, db, SubmitRequest{Profile: "p1", URL: "https://xn--bcher-kva.example/cart", Via: "click"})
	if err != nil {
		t.Fatal(err)
	}
	if m, name := card(it.ID); m["asciiHost"] != "xn--bcher-kva.example" || m["unicodeHost"] != "bücher.example" || name != "Submit on bücher.example (xn--bcher-kva.example)" {
		t.Errorf("submit item: %v %q", m, name)
	}
	id, _ := AskSite(ctx, db, "p1", KindOther, "https://shop.example/", nil)
	if m, name := card(id); m["asciiHost"] != nil || m["unicodeHost"] != nil || name != "Use shop.example" {
		t.Errorf("a plain host: %v %q", m, name)
	}
}

// An IPv6 literal is matched in one spelling, and an IPv4-mapped one as
// the IPv4 address it reaches: blocks on [::1] and 169.254.169.254 hold.
func TestIPv6LiteralsAreCanonical(t *testing.T) {
	pol := policyWith(t, LevelFullNoApprovals, "http://[::1]=block", "169.254.169.254=block")
	for _, u := range []string{
		"http://[::1]/", "http://[0:0:0:0:0:0:0:1]/", "http://[0::1]:8080/", "https://[0000::0001]/",
		"http://169.254.169.254/", "http://[::ffff:169.254.169.254]/", "http://[::ffff:a9fe:a9fe]/", "http://[::FFFF:A9FE:A9FE]:80/",
		"http://2852039166/",
	} {
		var be *SiteBlockedError
		if err := pol.Check(u); !errors.As(err, &be) {
			t.Errorf("%s: %v, want blocked", u, err)
		}
	}
	if err := pol.Check("http://[::2]/"); err != nil {
		t.Errorf("[::2]: %v", err)
	}
	for in, want := range map[string]string{
		"http://[0:0:0:0:0:0:0:1]:8080/": "http://[::1]:8080",
		"http://[::FFFF:a9fe:a9fe]/":     "http://169.254.169.254",
		"http://[2001:DB8:0::1]/":        "http://[2001:db8::1]",
	} {
		if got := Origin(in); got != want {
			t.Errorf("Origin(%q) = %q, want %q", in, got, want)
		}
	}
	if got, err := NormalizeAllowEntry("http://[0:0::1]:3000"); err != nil || got != "http://[::1]:3000" {
		t.Errorf("allow entry: %q %v", got, err)
	}
}

// BrowserOrigin is Chrome's own serialization, which the extension compares
// expectOrigin with; Origin stays the matching key.
func TestBrowserOriginIsChromes(t *testing.T) {
	for in, want := range map[string]string{
		"http://[::ffff:127.0.0.1]:8080/x":   "http://[::ffff:7f00:1]:8080",
		"http://[::FFFF:7F00:1]/":            "http://[::ffff:7f00:1]",
		"http://[0:0:0:0:0:0:0:1]:80/":       "http://[::1]",
		"http://[2001:db8:0:0:1:0:0:1]/":     "http://[2001:db8::1:0:0:1]",
		"http://[2001:DB8:0:1:0:0:0:1]/":     "http://[2001:db8:0:1::1]",
		"http://[1:2:3:4:5:6:7:8]/":          "http://[1:2:3:4:5:6:7:8]",
		"http://[::]/":                       "http://[::]",
		"http://[1::]/":                      "http://[1::]",
		"HTTPS://Bücher.Example.:443/a":      "https://xn--bcher-kva.example.",
		"http://2130706433:3000/":            "http://127.0.0.1:3000",
		"http://localhost:3000/app?x=1#frag": "http://localhost:3000",
	} {
		if got := BrowserOrigin(in); got != want {
			t.Errorf("BrowserOrigin(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Origin("http://[::ffff:7f00:1]:8080/"); got != "http://127.0.0.1:8080" {
		t.Errorf("Origin, the matching key: %q", got)
	}
}
