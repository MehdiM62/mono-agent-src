package credhttp_test

import (
	"errors"
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
		resp, err = c.Do(post)
		if code == http.StatusFound {
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		} else if !errors.Is(err, credhttp.ErrResend) || !strings.Contains(err.Error(), "redirected to "+strings.TrimPrefix(p.To, "http://")) {
			t.Errorf("%d: a POST's redirect elsewhere: %v, want it refused naming the target", code, err)
		}
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
	if err := credhttp.CheckRedirect(withBody, []*http.Request{first}); !errors.Is(err, credhttp.ErrResend) ||
		err.Error() != "redirected to other.example.test, not followed because it would resend the credential" {
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

// A credential in the URL (a key in the query, a pre-signed URL) doesn't
// reach another host in the Referer Go sets on each hop, for a Client's
// request and a marked one alike, on a 302 and a body-less 307.
func TestRedirectDropsTheReferer(t *testing.T) {
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		for name, c := range map[string]*http.Client{
			"Client": credhttp.Client(nil),
			"marked": {CheckRedirect: credhttp.CheckRedirect},
		} {
			p := credhttptest.NewPair(t, code, `{}`)
			req, _ := http.NewRequest(http.MethodGet, p.From+"/v1/items?api_key=q-secret", nil)
			if name == "marked" {
				req = credhttp.Mark(req)
			}
			resp, err := c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if got := p.Got(); len(got) != 1 || p.Leaked("q-secret") != "" {
				t.Errorf("%d %s: the other host got %+v", code, name, got)
			}
		}
	}
}

// A header stored under a key Go doesn't spell (h["x-api-key"]) is
// dropped too, named by Mark or not.
func TestRedirectDropsHeadersHoweverSpelled(t *testing.T) {
	for name, c := range map[string]*http.Client{
		"Client": credhttp.Client(nil),
		"marked": {CheckRedirect: credhttp.CheckRedirect},
	} {
		p := credhttptest.NewPair(t, http.StatusFound, `{}`)
		req, _ := http.NewRequest(http.MethodGet, p.From+"/me", nil)
		req.Header["x-api-key"] = []string{"lower-secret"}
		req.Header["authorization"] = []string{"Bearer lower-tok"}
		if name == "marked" {
			req = credhttp.Mark(req, "X-Api-Key")
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if where := p.Leaked("lower-secret") + p.Leaked("lower-tok"); where != "" || len(p.Got()) != 1 {
			t.Errorf("%s: %s reached the other host", name, where)
		}
	}
}

// A base client's own CheckRedirect is kept and runs first: its refusal
// stands, and what it lets through still loses the credential.
func TestClientKeepsTheBasesPolicy(t *testing.T) {
	refuse := errors.New("refused by the base")
	p := credhttptest.NewPair(t, http.StatusFound, `{}`)
	c := credhttp.Client(&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return refuse }})
	req, _ := http.NewRequest(http.MethodGet, p.From+"/me", nil)
	if _, err := c.Do(req); !errors.Is(err, refuse) || len(p.Got()) != 0 {
		t.Errorf("the base's refusal: %v, the other host got %+v", err, p.Got())
	}
	c = credhttp.Client(&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }})
	req, _ = http.NewRequest(http.MethodGet, p.From+"/me", nil)
	req.Header.Set("X-Api-Key", "base-secret")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if where := p.Leaked("base-secret"); where != "" {
		t.Errorf("past the base's policy, the key reached the other host (%s)", where)
	}
}
