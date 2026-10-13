package monomind

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/logging"
)

// The run a turn belongs to reaches monomind as MONOAGENT_RUN_ID.
func TestExecForwardsTheRunID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake monomind is a shell script")
	}
	t.Setenv(logging.EnvRunID, "inherited")
	bin := filepath.Join(t.TempDir(), "fake-runid.sh")
	script := "#!/bin/sh\necho \"run=$MONOAGENT_RUN_ID\" >&2\nexit 3\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context) string {
		var buf bytes.Buffer
		_, _ = Exec(ctx, ExecOptions{Bin: bin, Runtime: "claude", Prompt: "hi", Stderr: &buf}, nil)
		return strings.TrimSpace(buf.String())
	}
	if got := run(logging.WithRunID(context.Background(), "exec-9")); got != "run=exec-9" {
		t.Errorf("with a run in ctx: %q", got)
	}
	if got := run(context.Background()); got != "run=inherited" {
		t.Errorf("without one, the process's own: %q", got)
	}
}
