package chat

import (
	"strings"
	"testing"
)

func TestRedactTitleTextCredentialForms(t *testing.T) {
	for _, c := range []struct{ name, in, leak string }{
		{"authorization bearer", "Authorization: Bearer abcdef0123456789abcdef0123456789", "abcdef0123"},
		{"short bearer", "call it with Bearer s3cr3t9 please", "s3cr3t9"},
		{"bearer alone", "curl -H 'bearer tok_1234'", "tok_1234"},
		{"authorization basic", "Authorization: Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"},
		{"basic alone", "send Basic dXNlcjpwYXNz now", "dXNlcjpwYXNz"},
		{"authorization plain", "authorization=abc123def", "abc123def"},
		{"password pair after words", "please log into the admin panel with password: hunter2 and go", "hunter2"},
		{"passwd equals", "set passwd=Sup3rS3cret now", "Sup3rS3cret"},
		{"token equals", "GITHUB_TOKEN=ghx_short", "ghx_short"},
		{"secret quoted", `use secret="my pass phrase" here`, "my pass phrase"},
		{"api_key", "api_key: k-1234", "k-1234"},
		{"apikey", "apikey=zz99", "zz99"},
		{"x-api-key header", "x-api-key: zz99", "zz99"},
		{"cookie", "cookie: session=abc123", "abc123"},
		{"is form", "the password is hunter2", "hunter2"},
		{"url userinfo", "clone https://bob:pw0rd@git.example.com/r.git", "pw0rd"},
		{"url token userinfo", "clone https://ghp_short@git.example.com/r.git", "ghp_short"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := redactTitleText(c.in)
			if strings.Contains(got, c.leak) {
				t.Fatalf("%q leaks %q: %q", c.in, c.leak, got)
			}
			if !strings.Contains(got, "[redacted]") {
				t.Fatalf("%q = %q, want a redaction mark", c.in, got)
			}
		})
	}
}

func TestRedactTitleTextKeepsOrdinaryText(t *testing.T) {
	for _, in := range []string{
		"Apply to the Acme backend role",
		"a basic understanding of tokens is useful",
		"reset my password flow design",
		"fix the login page (see https://example.com/docs)",
		"email bob@example.com about the offer",
		"the bearer of bad news",
	} {
		want := strings.Join(strings.Fields(in), " ")
		got := redactTitleText(in)
		if in == "the bearer of bad news" {
			// Bearer takes the next word by design; the rest stays.
			if !strings.HasPrefix(got, "the bearer [redacted]") || !strings.HasSuffix(got, "bad news") {
				t.Errorf("%q = %q", in, got)
			}
			continue
		}
		if got != want {
			t.Errorf("%q changed to %q", in, got)
		}
	}
}
