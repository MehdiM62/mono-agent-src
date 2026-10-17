package mcp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/profiledir"
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

// node_run writes no hidden file in the user's home outside the profile's
// project folder (an autostart entry or a shell's startup file runs code
// later); in the project folder it writes.
func TestNodeRunWritesNoHiddenHomeFile(t *testing.T) {
	s := newTestServerAllowMutations(t, true)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(home, ".bashrc")
	write := func(path string) (string, error) {
		return callNamed(t, s, "node_run", map[string]interface{}{
			"type":   "data.write_binary_file",
			"config": map[string]interface{}{"file_path": path, "field": "text", "encoding": "utf8"},
			"input":  map[string]interface{}{"text": "echo hi"},
		})
	}
	if _, err := write(rc); err == nil || !strings.Contains(err.Error(), "hidden file or folder") {
		t.Fatalf("node_run to ~/.bashrc: %v", err)
	}
	if _, err := os.Stat(rc); err == nil {
		t.Fatal("node_run wrote ~/.bashrc")
	}
	out := filepath.Join(profiledir.Root(nil, serverTestProfile(s)), "exports", "out.txt")
	if _, err := write(out); err != nil {
		t.Fatalf("node_run into the project folder: %v", err)
	}
}
