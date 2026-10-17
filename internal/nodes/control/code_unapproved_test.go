package control

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A workflow the user has not approved runs no Python (a program on this
// computer); its JavaScript, which runs in goja with no way out, still runs.
func TestCodeRunsNoPythonInAnUnapprovedWorkflow(t *testing.T) {
	ctx := workflow.WithAIAuthored(context.Background())
	in := workflow.NodeInput{WorkflowID: "wf-1", Items: []workflow.Item{workflow.NewItem(map[string]interface{}{"a": 1})}}
	_, err := (&CodeNode{}).Execute(ctx, in, map[string]interface{}{"language": "python", "code": "import os\nos.system('true')"})
	if !errors.Is(err, workflow.ErrUnapprovedProgram) {
		t.Fatalf("python err = %v, want the unapproved-workflow refusal", err)
	}
	out, err := (&CodeNode{}).Execute(ctx, in, map[string]interface{}{"code": `[{ b: 2 }]`})
	if err != nil || len(out) != 1 || len(out[0].Items) != 1 {
		t.Fatalf("javascript: %+v, %v", out, err)
	}
}
