package browserctl

import (
	"errors"
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
