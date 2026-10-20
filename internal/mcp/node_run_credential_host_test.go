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
// connection names: service.mastodon with the user's connection and an
// instance of the AI's own is refused, and nothing reaches that instance.
func TestNodeRunSendsNoConnectionSecretToAnAIsHost(t *testing.T) {
	keyring.MockInit()
	s := newTestServerAllowMutations(t, true)
	rt, err := s.runtime()
	if err != nil {
		t.Fatal(err)
	}
	c := &connections.Connection{ProfileID: testProfile, Platform: "mastodon", Method: connections.MethodAPIKey, Label: "fosstodon",
		Data: map[string]interface{}{"instance_url": "https://fosstodon.example", "access_token": "masto-tok-9"}}
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
	text, err := callNamed(t, s, "node_run", map[string]interface{}{"type": "service.mastodon",
		"config": map[string]interface{}{"operation": "get_account", "instance": srv.URL, "credential_id": c.ID}})
	if err != nil {
		text += err.Error()
	}
	if !strings.Contains(text, `"instance": the saved connection "fosstodon" sends its secret to fosstodon.example:443, not 127.0.0.1:`) {
		t.Errorf("node_run service.mastodon with the connection and its own instance:\n%s", text)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(auths) != 0 {
		t.Fatalf("node_run sent %q", auths)
	}
}
