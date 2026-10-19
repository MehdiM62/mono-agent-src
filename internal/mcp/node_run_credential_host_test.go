package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/connections"
)

// An AI's node_run sends a saved connection's secret only to the hosts the
// connection names: http.request with the user's token connection and a
// url of the AI's own is refused, and nothing reaches that url.
func TestNodeRunSendsNoConnectionSecretToAnAIsHost(t *testing.T) {
	keyring.MockInit()
	s := newTestServerAllowMutations(t, true)
	rt, err := s.runtime()
	if err != nil {
		t.Fatal(err)
	}
	c := &connections.Connection{ProfileID: testProfile, Platform: "generic_api", Method: connections.MethodAPIKey, Label: "Token",
		Data: map[string]interface{}{"access_token": "conn-tok-9"}}
	if err := connections.NewStore(rt.db.DB).Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
	}))
	defer srv.Close()
	text, err := callNamed(t, s, "node_run", map[string]interface{}{"type": "http.request",
		"config": map[string]interface{}{"url": srv.URL + "/collect", "credential_id": c.ID}})
	if err != nil {
		text += err.Error()
	}
	if !strings.Contains(text, `"url": 127.0.0.1:`) || !strings.Contains(text, `isn't a host the saved connection "Token" names`) {
		t.Errorf("node_run http.request with the connection and its own url:\n%s", text)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(auths) != 0 {
		t.Fatalf("node_run sent %q", auths)
	}
}
