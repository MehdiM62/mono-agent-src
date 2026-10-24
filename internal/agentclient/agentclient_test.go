package agentclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
)

const tok = "mat-agentclient-test-token"

func codeOf(err error) string {
	var ae *aitask.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func TestFromEnvNeedsBothVariables(t *testing.T) {
	for _, c := range []struct{ url, token string }{{"", ""}, {"http://x", ""}, {"", tok}, {"  ", tok}} {
		t.Setenv(EnvURL, c.url)
		t.Setenv(EnvToken, c.token)
		if FromEnv() != nil {
			t.Errorf("url %q token %q turned client mode on", c.url, c.token)
		}
	}
	t.Setenv(EnvURL, "not a url")
	t.Setenv(EnvToken, tok)
	c := FromEnv()
	if c == nil {
		t.Fatal("a set pair with a bad URL turned client mode off")
	}
	if _, err := c.Task(context.Background(), "at-1"); codeOf(err) != CodeAPIUnavailable || strings.Contains(err.Error(), tok) {
		t.Errorf("bad URL: %v", err)
	}
}

// The token is sent to the URL it was given for, never on to a redirect target.
func TestTheTokenIsNotFollowedThroughARedirect(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != "" || r.Header.Get(taskTokenHeader) != ""
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/x", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	_, err := New(srv.URL, tok).Complete(context.Background(), "at-1", "tasktoken", []byte(`{}`))
	if leaked || err == nil {
		t.Fatalf("leaked %v, err %v", leaked, err)
	}
}

func TestRefusalsAreMappedAndScrubbed(t *testing.T) {
	status, body := 0, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := New(srv.URL, tok)
	for _, tc := range []struct {
		status int
		body   string
		code   string
	}{
		{422, `{"error":"bad ` + tok + `","code":"schema_invalid","reason":"r"}`, "schema_invalid"},
		{409, `{"error":"closed","code":"task_closed"}`, "task_closed"},
		{401, `{"error":"missing or invalid bearer credential"}`, CodeAPIRefused},
		{403, `{"error":"this token is not bound to that task"}`, CodeAPIRefused},
		{500, `<html>` + tok + `</html>`, CodeAPIUnavailable},
		{200, `not json`, CodeAPIUnavailable},
	} {
		status, body = tc.status, tc.body
		_, err := c.Task(context.Background(), "at-1")
		if codeOf(err) != tc.code || err == nil || strings.Contains(err.Error(), tok) {
			t.Errorf("%d %s: %v (code %q), want %q with no token", tc.status, tc.body, err, codeOf(err), tc.code)
		}
	}
	var ae *aitask.Error
	status, body = 422, `{"error":"x","code":"schema_invalid","reason":"summary is required"}`
	if _, err := c.Task(context.Background(), "at-1"); !errors.As(err, &ae) || ae.Fields["reason"] != "summary is required" {
		t.Errorf("fields were lost: %v", err)
	}
}
