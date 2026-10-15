package httpnodes

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func basicHeader(u, p string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(u+":"+p))
}

// Every manual auth_type puts the secret where it belongs.
func TestRequestAuthModes(t *testing.T) {
	cases := []struct {
		name   string
		cfg    map[string]interface{}
		header string // header to check
		want   string
		query  string // query param to check instead, when set
	}{
		{"basic", map[string]interface{}{"auth_type": "basic", "auth_username": "ann", "auth_password": "pw-1"}, "Authorization", basicHeader("ann", "pw-1"), ""},
		{"bearer", map[string]interface{}{"auth_type": "bearer", "auth_token": "tok-1"}, "Authorization", "Bearer tok-1", ""},
		{"api_key header", map[string]interface{}{"auth_type": "api_key", "auth_api_key_in": "header", "auth_api_key_name": "X-Api-Key", "auth_api_key_value": "k-1"}, "X-Api-Key", "k-1", ""},
		{"api_key default in", map[string]interface{}{"auth_type": "api_key", "auth_api_key_name": "X-Api-Key", "auth_api_key_value": "k-2"}, "X-Api-Key", "k-2", ""},
		{"api_key query", map[string]interface{}{"auth_type": "api_key", "auth_api_key_in": "query", "auth_api_key_name": "key", "auth_api_key_value": "k-3"}, "", "k-3", "key"},
		{"n8n header alias", map[string]interface{}{"auth_type": "httpHeaderAuth", "auth_api_key_name": "X-Token", "auth_api_key_value": "k-4"}, "X-Token", "k-4", ""},
		{"n8n query alias", map[string]interface{}{"auth_type": "httpQueryAuth", "auth_api_key_name": "token", "auth_api_key_value": "k-5"}, "", "k-5", "token"},
		{"none", map[string]interface{}{"auth_type": "none"}, "Authorization", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, nil)
			tc.cfg["url"] = rec.URL + "/x"
			mustOK(t, &RequestNode{}, tc.cfg)
			got := rec.last(t)
			if tc.query != "" {
				if v := got.Query[tc.query]; len(v) != 1 || v[0] != tc.want {
					t.Fatalf("query %s = %v, want %q", tc.query, v, tc.want)
				}
				return
			}
			if v := got.Header.Get(tc.header); v != tc.want {
				t.Fatalf("%s = %q, want %q", tc.header, v, tc.want)
			}
		})
	}
}

func TestRequestAuthRejectsUnknownType(t *testing.T) {
	_, e := execute(t, &RequestNode{}, map[string]interface{}{"url": "http://127.0.0.1:1/", "auth_type": "kerberos"})
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "auth_type") {
		t.Fatalf("want an auth_type error item, got %v", e)
	}
}

// Digest auth answers the server's challenge with a valid response hash.
func TestRequestDigestAuth(t *testing.T) {
	const realm, nonce, user, pw = "api", "abc123", "dana", "pw-9"
	h := func(s string) string { x := md5.Sum([]byte(s)); return hex.EncodeToString(x[:]) }
	calls := 0
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Digest ") {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm=%q, nonce=%q, qop="auth", opaque="op"`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		params := map[string]string{}
		for _, p := range splitAuthParams(strings.TrimPrefix(authz, "Digest ")) {
			k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
			params[k] = strings.Trim(v, `"`)
		}
		ha1 := h(user + ":" + realm + ":" + pw)
		ha2 := h(r.Method + ":" + r.URL.RequestURI())
		want := h(ha1 + ":" + nonce + ":" + params["nc"] + ":" + params["cnonce"] + ":auth:" + ha2)
		if params["response"] != want || params["opaque"] != "op" || params["username"] != user {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	m := mustOK(t, &RequestNode{}, map[string]interface{}{
		"url": rec.URL + "/secure?x=1", "method": "POST", "body": `{"a":1}`,
		"auth_type": "digest", "auth_username": user, "auth_password": pw,
	})
	if calls != 2 || m[0].JSON["status"] != 200 {
		t.Fatalf("calls=%d status=%v, want a challenge then 200", calls, m[0].JSON["status"])
	}
	if got := rec.last(t).Body; got != `{"a":1}` {
		t.Fatalf("the retried request lost its body: %q", got)
	}
}
