package httpnodes

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// ssh to this computer runs a program here, so a workflow the user has not
// approved is refused before it connects; ssh to another host still runs.
func TestSSHToThisComputerIsRefusedInAnUnapprovedWorkflow(t *testing.T) {
	ctx := workflow.WithAIAuthored(context.Background())
	port, seen := startExecServer(t, envAccept)
	cfg := map[string]interface{}{"host": "127.0.0.1", "port": float64(port), "username": "user",
		"password": "secret", "command": "echo hi", "timeout_seconds": float64(10)}
	in := workflow.NodeInput{WorkflowID: "wf-1", ExecutionID: "exec-1"}
	if _, err := (&SSHNode{}).Execute(ctx, in, cfg); !errors.Is(err, workflow.ErrUnapprovedProgram) {
		t.Fatalf("err = %v, want the unapproved-workflow refusal", err)
	}
	select {
	case s := <-seen:
		t.Fatalf("the refused step reached the server: %q", s.command)
	case <-time.After(200 * time.Millisecond):
	}
	old := sshAddrIsLocal
	t.Cleanup(func() { sshAddrIsLocal = old })
	sshAddrIsLocal = func(net.IP) bool { return false }
	if _, err := (&SSHNode{}).Execute(ctx, in, cfg); err != nil {
		t.Fatalf("another host: %v", err)
	}
	if s := <-seen; s.command != "echo hi" {
		t.Errorf("another host ran %q", s.command)
	}
}
