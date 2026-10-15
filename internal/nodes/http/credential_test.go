package httpnodes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// credCfg is a config as the engine leaves it after resolving
// credential_id: the connection's data under "credential" (and merged at
// the top level, which the node ignores).
func credCfg(url string, data map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{"url": url, "credential_id": "conn-1", "credential": data}
	for k, v := range data {
		cfg[k] = v
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

func TestRequestCredentialDerivedAuth(t *testing.T) {
	cases := []struct {
		name   string
		data   map[string]interface{}
		extra  map[string]interface{}
		header string
		want   string
		query  string
	}{
		{"oauth access_token", map[string]interface{}{"access_token": "at-1", "refresh_token": "rt"}, nil, "Authorization", "Bearer at-1", ""},
		{"api_key", map[string]interface{}{"api_key": "ak-1"}, nil, "Authorization", "Bearer ak-1", ""},
		{"token", map[string]interface{}{"token": "pat-1"}, nil, "Authorization", "Bearer pat-1", ""},
		{"secret_key", map[string]interface{}{"secret_key": "sk-1"}, nil, "Authorization", "Bearer sk-1", ""},
		{"username password", map[string]interface{}{"username": "u1", "password": "p1"}, nil, "Authorization", basicHeader("u1", "p1"), ""},
		{"atlassian email api_token", map[string]interface{}{"email": "a@b.c", "api_token": "t1"}, nil, "Authorization", basicHeader("a@b.c", "t1"), ""},
		{"twilio sid auth_token", map[string]interface{}{"account_sid": "AC1", "auth_token": "t2"}, nil, "Authorization", basicHeader("AC1", "t2"), ""},
		{"generic_api header_name", map[string]interface{}{"api_key": "g1", "header_name": "X-Service-Key"}, nil, "X-Service-Key", "g1", ""},
		{"generic_api Authorization header_name", map[string]interface{}{"api_key": "g2", "header_name": "Authorization"}, nil, "Authorization", "Bearer g2", ""},
		{"override header", map[string]interface{}{"api_key": "h1"}, map[string]interface{}{"credential_auth": "header:X-API-Key"}, "X-Api-Key", "h1", ""},
		{"override query", map[string]interface{}{"api_key": "q1"}, map[string]interface{}{"credential_auth": "query:api_key"}, "", "q1", "api_key"},
		{"override basic stripe-style", map[string]interface{}{"secret_key": "sk_test_1"}, map[string]interface{}{"credential_auth": "basic"}, "Authorization", basicHeader("sk_test_1", ""), ""},
		{"override bearer over basic pair", map[string]interface{}{"username": "u", "password": "p", "token": "tk"}, map[string]interface{}{"credential_auth": "bearer"}, "Authorization", "Bearer tk", ""},
		{"auth_type credential", map[string]interface{}{"api_key": "c1"}, map[string]interface{}{"auth_type": "credential"}, "Authorization", "Bearer c1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, nil)
			mustOK(t, &RequestNode{}, credCfg(rec.URL+"/r", tc.data, tc.extra))
			got := rec.last(t)
			if tc.query != "" {
				if v := got.Query[tc.query]; len(v) != 1 || v[0] != tc.want {
					t.Fatalf("query %s = %v, want %q", tc.query, v, tc.want)
				}
				if got.Header.Get("Authorization") != "" {
					t.Fatal("a query credential also sent Authorization")
				}
				return
			}
			if v := got.Header.Get(tc.header); v != tc.want {
				t.Fatalf("%s = %q, want %q", tc.header, v, tc.want)
			}
		})
	}
}

// Explicit auth_type fields win over a selected connection.
func TestRequestExplicitAuthBeatsCredential(t *testing.T) {
	rec := newRecorder(t, nil)
	mustOK(t, &RequestNode{}, credCfg(rec.URL, map[string]interface{}{"api_key": "conn-key"},
		map[string]interface{}{"auth_type": "bearer", "auth_token": "explicit"}))
	if got := rec.last(t).Header.Get("Authorization"); got != "Bearer explicit" {
		t.Fatalf("Authorization = %q", got)
	}
}

// Errors name the problem and the connection's field names, never values.
func TestRequestCredentialErrorsDoNotLeakSecrets(t *testing.T) {
	cases := []map[string]interface{}{
		credCfg("http://127.0.0.1:1/", map[string]interface{}{"host": "db", "port": "5432", "private_key": "PRIVATE-VALUE"}, nil),
		credCfg("http://127.0.0.1:1/", map[string]interface{}{"api_key": "PRIVATE-VALUE"}, map[string]interface{}{"credential_auth": "header:"}),
		credCfg("http://127.0.0.1:1/", map[string]interface{}{"api_key": "PRIVATE-VALUE"}, map[string]interface{}{"credential_auth": "oauth1"}),
	}
	for i, cfg := range cases {
		_, e := execute(t, &RequestNode{}, cfg)
		if len(e) != 1 {
			t.Fatalf("case %d: want one error item, got %v", i, e)
		}
		msg := e[0].JSON["error"].(string)
		if strings.Contains(msg, "PRIVATE-VALUE") || !strings.Contains(msg, "conn-1") {
			t.Fatalf("case %d: error %q leaks a secret or does not name the connection", i, msg)
		}
	}
}

// auth_type credential with a connection the engine could not resolve
// (no config["credential"]) is an error, not an unauthenticated request.
func TestRequestUnresolvedCredentialFails(t *testing.T) {
	rec := newRecorder(t, nil)
	_, e := execute(t, &RequestNode{}, map[string]interface{}{"url": rec.URL, "auth_type": "credential", "credential_id": "gone"})
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), `"gone" did not resolve`) {
		t.Fatalf("want an unresolved-credential error, got %v", e)
	}
	if len(rec.all()) != 0 {
		t.Fatal("the request was sent without its credential")
	}
}

// A path-only url is completed with the connection's base_url.
func TestRequestCredentialBaseURL(t *testing.T) {
	rec := newRecorder(t, nil)
	mustOK(t, &RequestNode{}, credCfg("/v1/items?limit=2", map[string]interface{}{"base_url": rec.URL + "/api/", "api_key": "b1"}, nil))
	got := rec.last(t)
	if got.Path != "/api/v1/items" || got.Query["limit"][0] != "2" {
		t.Fatalf("request went to %s ?%v", got.Path, got.Query)
	}
	_, e := execute(t, &RequestNode{}, map[string]interface{}{"url": "/v1/items"})
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "not an absolute URL") {
		t.Fatalf("a path without a connection: %v", e)
	}
}

// redirectPair starts a target on localhost and an origin on 127.0.0.1
// that redirects to it (another host to net/http). It returns the origin
// URL and a func giving the headers the target saw.
func redirectPair(t *testing.T, sameHost bool) (string, func() http.Header) {
	t.Helper()
	var seen http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	var origin *httptest.Server
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			seen = r.Header.Clone()
			return
		}
		if sameHost {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		http.Redirect(w, r, targetURL+"/final", http.StatusFound)
	}))
	t.Cleanup(origin.Close)
	return origin.URL + "/start", func() http.Header { return seen }
}

// Credential-derived headers do not follow a redirect to another host,
// unless send_credentials_on_redirect; on the same host they do.
func TestRequestCredentialHeadersOnRedirect(t *testing.T) {
	data := map[string]interface{}{"api_key": "rk-1"}
	t.Run("custom header stripped cross-host", func(t *testing.T) {
		u, seen := redirectPair(t, false)
		mustOK(t, &RequestNode{}, credCfg(u, data, map[string]interface{}{"credential_auth": "header:X-Api-Key"}))
		if seen() == nil || seen().Get("X-Api-Key") != "" {
			t.Fatalf("target saw X-Api-Key %q", seen().Get("X-Api-Key"))
		}
	})
	t.Run("bearer stripped cross-host", func(t *testing.T) {
		u, seen := redirectPair(t, false)
		mustOK(t, &RequestNode{}, credCfg(u, data, nil))
		if seen().Get("Authorization") != "" {
			t.Fatal("Authorization followed the redirect")
		}
	})
	t.Run("custom config header stripped cross-host", func(t *testing.T) {
		u, seen := redirectPair(t, false)
		mustOK(t, &RequestNode{}, map[string]interface{}{"url": u, "headers": `{"X-Private": "v"}`})
		if seen().Get("X-Private") != "" {
			t.Fatal("a configured header followed the redirect")
		}
	})
	t.Run("kept on the same host", func(t *testing.T) {
		u, seen := redirectPair(t, true)
		mustOK(t, &RequestNode{}, credCfg(u, data, map[string]interface{}{"credential_auth": "header:X-Api-Key"}))
		if seen().Get("X-Api-Key") != "rk-1" {
			t.Fatal("the header was dropped on a same-host redirect")
		}
	})
	t.Run("send_credentials_on_redirect", func(t *testing.T) {
		u, seen := redirectPair(t, false)
		mustOK(t, &RequestNode{}, credCfg(u, data, map[string]interface{}{"send_credentials_on_redirect": true}))
		if seen().Get("Authorization") != "Bearer rk-1" {
			t.Fatalf("Authorization = %q, want it kept", seen().Get("Authorization"))
		}
	})
	t.Run("follow_redirects false", func(t *testing.T) {
		u, _ := redirectPair(t, false)
		m, e := execute(t, &RequestNode{}, map[string]interface{}{"url": u, "follow_redirects": false, "never_error": true})
		if len(e) != 0 || m[0].JSON["status"] != http.StatusFound {
			t.Fatalf("want the 302 itself, got %v %v", m, e)
		}
	})
}

// GraphQL uses the same credential derivation.
func TestGraphQLCredentialAuth(t *testing.T) {
	rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":{"ok":true}}`)) })
	cfg := credCfg("", map[string]interface{}{"access_token": "gq-1"}, map[string]interface{}{"endpoint": rec.URL + "/graphql", "query": "{ ok }"})
	delete(cfg, "url")
	mustOK(t, &GraphQLNode{}, cfg, workflow.NewItem(map[string]interface{}{}))
	if got := rec.last(t).Header.Get("Authorization"); got != "Bearer gq-1" {
		t.Fatalf("Authorization = %q", got)
	}
}
