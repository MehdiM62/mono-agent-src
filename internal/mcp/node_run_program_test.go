package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// An AI wrote a node_run's config, so the node runs as in a workflow the user
// has not approved: no program on this computer; other nodes run as before
// (TestNodeRunRunsOneNode).
func TestNodeRunRunsNoProgram(t *testing.T) {
	if _, err := exec.LookPath("touch"); err != nil {
		t.Skip("uses touch")
	}
	s := newTestServerAllowMutations(t, true)
	ran := filepath.Join(t.TempDir(), "ran")
	wantErr(t, s, "node_run", map[string]interface{}{
		"type":   "system.execute_command",
		"config": map[string]interface{}{"command": "touch", "args": []interface{}{ran}},
	}, "which node_run never does for an AI")
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("node_run ran the command")
	}
}
