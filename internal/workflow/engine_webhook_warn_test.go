package workflow

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// A one-shot command (workflow activate, deactivate, run) finds the webhook
// port taken whenever the daemon is up; that is expected and must not print
// a warning. The daemon, which should own the port, still warns.
func TestWebhookPortInUseWarnsOnlyForDaemon(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Setenv("MONOAGENT_WEBHOOK_ADDR", "")

	for _, daemon := range []bool{false, true} {
		var buf bytes.Buffer
		store := newFullEngineStore(t)
		eng := NewWorkflowEngineWithStore(store, store.RawDB(), &fakeScheduler{}, NewNodeTypeRegistry(),
			EngineConfig{ProfileID: "default", WebhookAddr: ln.Addr().String(), AllowAllProfiles: daemon},
			zerolog.New(&buf).Level(zerolog.InfoLevel))
		if err := eng.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		_ = eng.Stop()
		if warned := strings.Contains(buf.String(), "webhook port already in use"); warned != daemon {
			t.Errorf("daemon=%v: warned=%v, log:\n%s", daemon, warned, buf.String())
		}
	}
}
