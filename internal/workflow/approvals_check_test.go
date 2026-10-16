package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// flakyApprovals is a store whose approvals check fails its first fails
// times.
type flakyApprovals struct {
	*SQLiteWorkflowStore
	fails int
}

func (f *flakyApprovals) DefinitionApproved(ctx context.Context, workflowID, hash string) (bool, error) {
	if f.fails > 0 {
		f.fails--
		return false, errors.New("database is locked")
	}
	return f.SQLiteWorkflowStore.DefinitionApproved(ctx, workflowID, hash)
}

// programProbe is a step that runs a program: it records its refusal.
type programProbe struct{ refused *error }

func (programProbe) Type() string { return "test.programprobe" }
func (n programProbe) Execute(ctx context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	*n.refused = RefuseUnapprovedProgram(ctx, "test.programprobe", in.WorkflowID)
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// A failed check of the user's approval is tried once more, so a transient
// store error doesn't refuse an approved workflow's steps; one that persists
// refuses them, and the refusal names it (#831 round 5, C4).
func TestAFailedApprovalCheckIsRetriedAndNamed(t *testing.T) {
	_, sqlStore := approvalsTestDB(t)
	ctx := context.Background()
	var refused error
	reg := NewNodeTypeRegistry()
	reg.Register("test.programprobe", func() NodeExecutor { return programProbe{refused: &refused} })
	wf := &Workflow{ID: "wf-mine", Name: "Mine", ProfileID: "default",
		Nodes: []WorkflowNode{
			{ID: "wf-mine-t", WorkflowID: "wf-mine", Type: "trigger.schedule", Name: "Every hour"},
			{ID: "wf-mine-p", WorkflowID: "wf-mine", Type: "test.programprobe", Name: "Run"},
		},
		Connections: []WorkflowConnection{{ID: "wf-mine-c", WorkflowID: "wf-mine", SourceNodeID: "wf-mine-t", SourceHandle: "main", TargetNodeID: "wf-mine-p", TargetHandle: "main"}},
	}
	createSQLWorkflow(t, sqlStore, wf)
	if err := sqlStore.ApproveDefinition(ctx, wf.ID); err != nil {
		t.Fatal(err)
	}
	run := func(fails int) error {
		t.Helper()
		cur, err := sqlStore.GetWorkflow(ctx, wf.ID)
		if err != nil {
			t.Fatal(err)
		}
		dag, err := BuildDAG(cur.Nodes, cur.Connections)
		if err != nil {
			t.Fatal(err)
		}
		refused = nil
		exec := &WorkflowExecution{ID: uuid.NewString(), WorkflowID: wf.ID, TriggerType: "trigger.schedule", TriggerNodeID: "wf-mine-t"}
		if err := RunExecution(ctx, exec, cur, dag, reg, &flakyApprovals{sqlStore, fails}, nil, NewExpressionEngine(), zerolog.Nop()); err != nil {
			t.Fatalf("RunExecution: %v", err)
		}
		return refused
	}
	if err := run(1); err != nil {
		t.Errorf("one failed check of an approved workflow refused its step: %v", err)
	}
	err := run(2)
	if !errors.Is(err, ErrUnapprovedProgram) || !strings.Contains(err.Error(), "database is locked") {
		t.Errorf("a check that keeps failing: %v", err)
	}
}
