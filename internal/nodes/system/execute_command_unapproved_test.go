package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A workflow the user has not approved (the chat, an agent or another
// workflow wrote or changed it) runs no command: the step is refused before
// anything starts, and says how to approve the workflow. The same step of an
// approved workflow runs.
func TestExecuteCommandIsRefusedInAnUnapprovedWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses touch")
	}
	ran := filepath.Join(t.TempDir(), "ran")
	cfg := map[string]interface{}{"command": "touch", "args": []interface{}{ran}}
	in := workflow.NodeInput{WorkflowID: "wf-1", ExecutionID: "exec-1"}
	_, err := (&ExecuteCommandNode{}).Execute(workflow.WithAIAuthored(context.Background()), in, cfg)
	if !errors.Is(err, workflow.ErrUnapprovedProgram) || !strings.Contains(err.Error(), "workflow activate wf-1") {
		t.Fatalf("err = %v, want the unapproved-workflow refusal naming wf-1", err)
	}
	if _, serr := os.Stat(ran); serr == nil {
		t.Fatal("the command ran")
	}
	if _, err := (&ExecuteCommandNode{}).Execute(context.Background(), in, cfg); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(ran); serr != nil {
		t.Fatalf("an approved workflow's command did not run: %v", serr)
	}
}
