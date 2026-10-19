package credhttp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp"
	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// A Client's request leaves its credential behind on a redirect to another
// host: every header but the standard ones on a GET's redirect, and the
// whole request on a 307 or 308 that would send its body again.
func TestClientKeepsTheCredentialOnRedirect(t *testing.T) {
	c := credhttp.Client(nil)
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		p := credhttptest.NewPair(t, code, `{}`)
		get, _ := http.NewRequest(http.MethodGet, p.From+"/me", nil)
		get.Header.Set("X-Api-Key", "k-secret")
		get.Header.Set("Authorization", "Bearer t-secret")
		get.Header.Set("Accept", "application/json")
		resp, err := c.Do(get)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		post, _ := http.NewRequest(http.MethodPost, p.From+"/token", strings.NewReader("client_secret=s-secret"))
		post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if resp, err = c.Do(post); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		// A 302 turns the POST into a GET without its body, which may go.
		want := 1
		if code == http.StatusFound {
			want = 2
		}
		got := p.Got()
		if len(got) != want || got[0].Method != http.MethodGet || got[0].Header.Get("Accept") != "application/json" {
			t.Fatalf("%d: the other host got %+v, want %d GETs", code, got, want)
		}
		for _, s := range []string{"k-secret", "t-secret", "s-secret"} {
			if where := p.Leaked(s); where != "" {
				t.Errorf("%d: %s reached the other host (%s)", code, s, where)
			}
		}
	}
}

// The policy's decision: leaving the host, its port, or https keeps none
// of a marked request's credential headers; staying keeps them. A marked
// request with a body isn't followed elsewhere; an unmarked one follows,
// without Authorization.
func TestCheckRedirectDecision(t *testing.T) {
	first := credhttp.Mark(httptest.NewRequest("GET", "https://api.example.test/x", nil), "Authorization", "X-Api-Key")
	for target, kept := range map[string]bool{
		"https://api.example.test/y":      true,
		"http://api.example.test/y":       false,
		"https://api.example.test:8443/y": false,
		"https://other.example.test/y":    false,
	} {
		req := httptest.NewRequest("GET", target, nil)
		req.Header.Set("Authorization", "Bearer t")
		req.Header.Set("X-Api-Key", "k")
		if err := credhttp.CheckRedirect(req, []*http.Request{first}); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if got := req.Header.Get("Authorization") != "" && req.Header.Get("X-Api-Key") != ""; got != kept {
			t.Errorf("%s: headers %v, want the credential kept %v", target, req.Header, kept)
		}
	}
	withBody := httptest.NewRequest("POST", "https://other.example.test/y", strings.NewReader("k=v"))
	if err := credhttp.CheckRedirect(withBody, []*http.Request{first}); err != http.ErrUseLastResponse {
		t.Errorf("a marked request's body to another host: %v", err)
	}
	plain := httptest.NewRequest("GET", "https://api.example.test/x", nil)
	other := httptest.NewRequest("POST", "https://other.example.test/y", strings.NewReader("k=v"))
	other.Header.Set("Authorization", "Bearer t")
	other.Header.Set("X-Trace", "1")
	if err := credhttp.CheckRedirect(other, []*http.Request{plain}); err != nil || other.Header.Get("Authorization") != "" || other.Header.Get("X-Trace") != "1" {
		t.Errorf("an unmarked request: %v %v", err, other.Header)
	}
}
