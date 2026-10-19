package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A redirect from https to http is refused for a GET too, so a same-host downgrade cannot carry
// the Authorization header in the clear.
func TestHTTPSToHTTPRedirectIsRefused(t *testing.T) {
	var seenAuth atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			seenAuth.Add(1)
		}
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
	}))
	defer secure.Close()

	hc := secure.Client()
	hc.CheckRedirect = noRedirectWithABody
	req, _ := http.NewRequest(http.MethodGet, secure.URL+"/x", nil)
	req.Header.Set("Authorization", "Bearer at-secret")
	resp, err := hc.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "https to http") {
		t.Fatalf("err = %v, want the downgrade refused", err)
	}
	if seenAuth.Load() != 0 {
		t.Error("the Authorization header reached the plain server")
	}
}

// An https to https redirect of a GET still works.
func TestHTTPSToHTTPSRedirectIsFollowed(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer target.Close()
	hc := target.Client()
	hc.CheckRedirect = noRedirectWithABody
	src := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer src.Close()
	// Both test servers use per-server certificates; trust the target's pool for the whole chain.
	hc.Transport = &http.Transport{TLSClientConfig: target.Client().Transport.(*http.Transport).TLSClientConfig.Clone()}
	hc.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true
	resp, err := hc.Get(src.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

// A read the library lets follow its redirect carries no bearer to
// another port of the same host or to a subdomain, as it would under Go's
// hostname-only rule (credhttp.Client).
func TestReadsCarryNoBearerToAnotherPortOrSubdomain(t *testing.T) {
	var seenAuth, hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "" {
			seenAuth.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusFound)
	}))
	defer api.Close()
	c, err := NewClient(api.URL, &fixedStore{&Token{AccessToken: "at-secret", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = c.getJSON(context.Background(), "/api/v1/me", &out)
	if n := seenAuth.Load(); n != 0 || hits.Load() == 0 {
		t.Errorf("the bearer reached another port of the same host %d time(s) in %d request(s)", n, hits.Load())
	}

	first := httptest.NewRequest(http.MethodGet, "https://monoes.me/api/v1/me", nil)
	next := httptest.NewRequest(http.MethodGet, "https://evil.monoes.me/api/v1/me", nil)
	next.Header.Set("Authorization", "Bearer at-secret")
	if err := c.HTTP.CheckRedirect(next, []*http.Request{first}); err != nil || next.Header.Get("Authorization") != "" {
		t.Errorf("to a subdomain: %v, Authorization %q", err, next.Header.Get("Authorization"))
	}
}

// fixedStore is a TokenStore holding one token.
type fixedStore struct{ t *Token }

func (s *fixedStore) Load(context.Context) (*Token, error)   { return s.t, nil }
func (s *fixedStore) Save(_ context.Context, t *Token) error { s.t = t; return nil }
func (s *fixedStore) Delete(context.Context) error           { s.t = nil; return nil }
