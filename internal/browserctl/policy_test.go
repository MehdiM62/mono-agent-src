package browserctl

import (
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/testdb"
)

func TestPolicyAllowsLocalAndDevHosts(t *testing.T) {
	p := Policy{Agent: true}
	allowed := []string{
		"http://localhost:3000/", "https://localhost", "http://LOCALHOST:8080/x?y=1",
		"http://app.localhost:5173", "http://127.0.0.1:8000", "http://127.9.9.9/", "http://[::1]:3000/",
		"http://printer.local/", "https://shop.test/cart", "http://10.0.0.5:8080", "http://172.16.4.1",
		"http://172.31.255.255", "http://192.168.1.20:3000", "http://localhost.:3000/",
	}
	for _, u := range allowed {
		if err := p.Check(u); err != nil {
			t.Errorf("%s: refused: %v", u, err)
		}
	}
	refused := []string{
		"https://example.com", "http://172.32.0.1", "http://11.0.0.1", "http://8.8.8.8",
		"https://localhost.example.com", "https://test.example.com", "http://[2001:db8::1]/",
		"file:///etc/passwd", "chrome://settings", "edge://settings", "chrome-extension://abc/x.html",
		"about:blank", "javascript:alert(1)", "data:text/html,hi", "ftp://localhost/", "not a url",
	}
	for _, u := range refused {
		err := p.Check(u)
		var na *NotAllowedError
		if !errors.As(err, &na) {
			t.Errorf("%s: allowed, want refused", u)
		}
	}
}

func TestPolicyHumanIsUnrestricted(t *testing.T) {
	p := Policy{}
	for _, u := range []string{"https://bank.example", "file:///etc/passwd", "chrome://settings"} {
		if !p.Allowed(u) {
			t.Errorf("%s: a human was refused", u)
		}
	}
	if p.SubmitGated("https://bank.example") {
		t.Error("a human's submit is gated")
	}
}

func TestPolicyAllowlist(t *testing.T) {
	p := Policy{Agent: true, Allowlist: []string{"https://example.com", "http://staging.acme.io:8080", "*.corp.dev"}}
	cases := map[string]bool{
		"https://example.com/":           true,
		"https://example.com:443/login":  true,
		"http://example.com/":            false, // scheme differs
		"https://example.com:8443/":      false, // port differs
		"https://www.example.com/":       false, // an origin entry is exact
		"http://staging.acme.io:8080/a":  true,
		"http://staging.acme.io/":        false,
		"https://corp.dev/":              true,
		"https://a.b.corp.dev:9000/":     true,
		"http://api.corp.dev/":           true,
		"https://evilcorp.dev/":          false,
		"https://corp.dev.evil.example/": false,
	}
	for u, want := range cases {
		if got := p.Allowed(u); got != want {
			t.Errorf("%s: allowed=%v, want %v", u, got, want)
		}
	}
}

func TestSubmitGatedOnlyOnAllowedNonLocalPages(t *testing.T) {
	p := Policy{Agent: true, Allowlist: []string{"https://example.com"}}
	if !p.SubmitGated("https://example.com/form") {
		t.Error("an allowlisted remote origin is not gated")
	}
	for _, u := range []string{"http://localhost:3000/form", "http://192.168.0.2/", "https://other.example/"} {
		if p.SubmitGated(u) {
			t.Errorf("%s: gated", u)
		}
	}
}

func TestNormalizeAllowEntry(t *testing.T) {
	good := map[string]string{
		"https://Example.com":       "https://example.com",
		"https://example.com/":      "https://example.com",
		"https://example.com:443":   "https://example.com",
		"http://example.com:8080":   "http://example.com:8080",
		" *.Corp.Example.com ":      "*.corp.example.com",
		"http://[::1]:3000":         "http://[::1]:3000",
		"https://xn--bcher-kva.com": "https://xn--bcher-kva.com",
	}
	for in, want := range good {
		got, err := NormalizeAllowEntry(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"example.com", "*", "*.", "*.com", "*.exa mple.com", "https://example.com/path", "https://u:p@example.com",
		"https://example.com/?q=1", "file:///tmp", "chrome://settings", "ftp://example.com", "", "https://",
	} {
		if got, err := NormalizeAllowEntry(bad); err == nil {
			t.Errorf("%q: accepted as %q", bad, got)
		}
	}
}

func TestAllowlistRoundTrip(t *testing.T) {
	db := testdb.Open(t).DB
	got, err := LoadAllowlist(db)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty allowlist: %v, %v", got, err)
	}
	if err := SaveAllowlist(db, []string{"https://b.example", "*.a.example", "https://B.example/"}); err != nil {
		t.Fatal(err)
	}
	got, err = LoadAllowlist(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "*.a.example" || got[1] != "https://b.example" {
		t.Fatalf("allowlist = %v", got)
	}
	if err := SaveAllowlist(db, []string{"nope"}); err == nil {
		t.Fatal("a bad entry was saved")
	}
}

func TestOrigin(t *testing.T) {
	cases := map[string]string{
		"https://Example.com:443/a?b": "https://example.com",
		"http://localhost:3000/x":     "http://localhost:3000",
		"http://[::1]:80/":            "http://[::1]",
		"about:blank":                 "about:blank",
		"file:///etc/passwd?x":        "file:///etc/passwd",
	}
	for in, want := range cases {
		if got := Origin(in); got != want {
			t.Errorf("Origin(%q) = %q, want %q", in, got, want)
		}
	}
}
