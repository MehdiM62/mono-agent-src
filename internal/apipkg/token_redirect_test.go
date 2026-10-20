package apipkg

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// tokenRedirect is a token endpoint that redirects every request to other,
// a second local server that records what reaches it.
type tokenRedirect struct {
	api, other *httptest.Server
	mu         sync.Mutex
	got        []string // method, Authorization, X-Token and body of each request other got
}

func newTokenRedirect(t *testing.T, code int) *tokenRedirect {
	t.Helper()
	tr := &tokenRedirect{}
	tr.other = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		tr.mu.Lock()
		tr.got = append(tr.got, r.Method+" auth="+r.Header.Get("Authorization")+" token="+r.Header.Get("X-Token")+" body="+string(b))
		tr.mu.Unlock()
		_, _ = w.Write([]byte(`{"id":"t1"}`))
	}))
	tr.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tr.other.URL+"/landing", code)
	}))
	t.Cleanup(tr.other.Close)
	t.Cleanup(tr.api.Close)
	return tr
}

func (tr *tokenRedirect) seen() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]string(nil), tr.got...)
}

// The requests a credential goes on outside an op's (the OAuth
// client-credentials token request, STS AssumeRole, an OAuth metadata
// fetch) follow no 307 or 308 to another host with their body, and a
// redirect that is followed carries none of the credential's headers.
func TestTokenRequestsKeepTheCredentialOnRedirect(t *testing.T) {
	ccCache.Range(func(k, _ any) bool { ccCache.Delete(k); return true })
	for _, code := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		tr := newTokenRedirect(t, code)
		for _, auth := range []string{"", "basic"} {
			c := &Credential{Platform: "cc", OAuth: &OAuthSpec{TokenURL: tr.api.URL + "/token", ClientAuth: auth}}
			if _, err := clientCredentialsToken(context.Background(), c, map[string]any{"client_id": "id1", "client_secret": "s3cret"}); err == nil {
				t.Errorf("%d, client auth %q: the token request followed the redirect", code, auth)
			}
		}
		base := sigV4Params{Service: "sts", Region: "us-east-1", AccessKeyID: "AKID", SecretAccessKey: "sk", SessionToken: "sess-1"}
		if _, err := assumeRole(context.Background(), tr.api.URL, base, "arn:aws:iam::1:role/r", "", "s"); err == nil {
			t.Errorf("%d: AssumeRole followed the redirect", code)
		}
		if got := tr.seen(); len(got) != 0 {
			t.Errorf("%d: the other host got %q", code, got)
		}
	}

	// A GET (the metadata fetch) has no body to resend: it follows, without
	// the credential's headers.
	tr := newTokenRedirect(t, http.StatusFound)
	m := &MetadataSpec{URL: tr.api.URL + "/me", Headers: map[string]string{"Authorization": "Bearer {{ cred.access_token }}", "X-Token": "{{ cred.access_token }}"},
		Fields: map[string]string{"tenant": "id"}}
	if _, err := fetchMetadata(context.Background(), m, map[string]any{"access_token": "at-1"}); err != nil {
		t.Fatal(err)
	}
	if got := tr.seen(); len(got) != 1 || strings.Contains(got[0], "at-1") {
		t.Errorf("the metadata fetch's redirect carried %q", got)
	}
}
