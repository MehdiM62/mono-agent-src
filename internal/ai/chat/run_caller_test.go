package chat

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/nodes/browserjev"
)

// The `workflow run` a chat tool call starts is marked as the chat's, so
// browser.jev holds its form submits for approval.
func TestRunSelfExecMarksTheChatAsCaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake monoagentcli is a shell script")
	}
	t.Setenv(browserjev.EnvAICaller, "")
	bin := filepath.Join(t.TempDir(), "fake-cli.sh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"caller=$"+browserjev.EnvAICaller+" args=$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runSelfExec(context.Background(), bin, "workflow", "run", "wf-1")
	if err != nil || strings.TrimSpace(string(out)) != "caller=chat args=workflow run wf-1" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
}
