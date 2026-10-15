package applicationsnodes_test

import (
	"context"
	"strings"
	"testing"

	applicationsnodes "github.com/monoes/mono-agent/internal/nodes/applications"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// The node acts only on the execution's profile: an explicit profile_id that
// differs is refused, and a run without any profile is an error, not "default".
func TestNodesRequireExecutionProfile(t *testing.T) {
	applicationsnodes.SetGlobalStore(newTestDB(t).DB)
	cases := []struct {
		name string
		node workflow.NodeExecutor
		cfg  map[string]interface{}
	}{
		{"list", &applicationsnodes.ListNode{}, nil},
		{"set_status", &applicationsnodes.SetStatusNode{}, map[string]interface{}{"id": "x", "status": "shortlisted"}},
		{"create", &applicationsnodes.CreateNode{}, map[string]interface{}{"kind": "job", "title": "T", "company": "C", "url": "https://x.example"}},
		{"tag", &applicationsnodes.TagNode{}, map[string]interface{}{"id": "x", "tag": "t"}},
	}
	for _, c := range cases {
		withA := vault.ContextWithProfileID(context.Background(), "A")
		cfg := map[string]interface{}{"profile_id": "B"}
		for k, v := range c.cfg {
			cfg[k] = v
		}
		if _, err := c.node.Execute(withA, workflow.NodeInput{}, cfg); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Errorf("%s: profile_id B under profile A: err = %v, want mismatch error", c.name, err)
		}
		delete(cfg, "profile_id")
		if _, err := c.node.Execute(context.Background(), workflow.NodeInput{}, cfg); err == nil || !strings.Contains(err.Error(), c.node.Type()+" ran without a profile") {
			t.Errorf("%s: no profile in ctx: err = %v, want 'ran without a profile'", c.name, err)
		}
	}
}
