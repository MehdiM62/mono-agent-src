package apipkg

import (
	"net/http"
	"strings"
	"testing"
)

func TestClientCredentialsTokenFetchedCachedAndInjected(t *testing.T) {
	ccCache.Range(func(k, _ any) bool { ccCache.Delete(k); return true })
	p := mustParse(t, `{"$schema":"apipkg.v1","id":"cc","title":"CC","base_url":"https://{{ cred.domain }}/api",
	  "credentials":[{"platform":"cc","methods":["client_credentials"],
	    "fields":[{"key":"domain","label":"Domain","required":true},{"key":"client_id","label":"Id","required":true},{"key":"client_secret","label":"Secret","secret":true,"required":true}],
	    "oauth":{"token_url":"https://{{ cred.domain }}/identity/connect/token","scopes":["api.organization"],"extra_params":{"deviceType":"2"}},
	    "inject":{"headers":{"Authorization":"Bearer {{ cred.access_token }}"}}}],
	  "operations":[{"resource":"","operation":"get","title":"Get","method":"GET","path":"/things"}]}`)
	seen := pagedServer(t, func(n int, r *http.Request, w http.ResponseWriter) {
		if strings.HasSuffix(r.URL.Path, "/connect/token") {
			r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "id1" ||
				r.Form.Get("client_secret") != "s1" || r.Form.Get("scope") != "api.organization" || r.Form.Get("deviceType") != "2" {
				t.Errorf("token form: %v", r.Form)
			}
			w.Write([]byte(`{"access_token":"tok1","expires_in":3600}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok1" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"ok":true}`))
	})
	creds := map[string]any{"domain": "vault.example.test", "client_id": "id1", "client_secret": "s1"}
	for i := 0; i < 2; i++ {
		if _, err := runPackageOp(p, map[string]any{"operation": "get"}, creds, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(*seen) != 3 {
		t.Fatalf("want 1 token request + 2 API requests (token cached), got %d", len(*seen))
	}
	if (*seen)[0].URL.Path != "/identity/connect/token" {
		t.Errorf("token URL = %s", (*seen)[0].URL)
	}
}

func TestClientCredentialsValidation(t *testing.T) {
	_, err := Parse([]byte(`{"$schema":"apipkg.v1","id":"cc","title":"CC","base_url":"https://x.test",
	  "credentials":[{"platform":"cc","methods":["client_credentials"],"fields":[{"key":"key","label":"K"}],
	    "oauth":{"token_url":"https://x.test/token"},"inject":{"headers":{"Authorization":"Bearer {{ cred.access_token }}"}}}],
	  "operations":[{"resource":"","operation":"get","title":"Get","method":"GET","path":"/t"}]}`), "test")
	if err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Errorf("want missing client_id/client_secret fields error, got %v", err)
	}
}
