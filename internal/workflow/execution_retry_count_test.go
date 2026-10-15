package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
)

// flakyNode fails its first `fails` attempts, then passes its input on.
type flakyNode struct{ calls, fails *int }

func (flakyNode) Type() string { return "test.flaky" }
func (f flakyNode) Execute(_ context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	*f.calls++
	if *f.calls <= *f.fails {
		return nil, errors.New("connection reset")
	}
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// QA-A U2: workflow_execution_nodes.retry_count stayed 0 however often a
// node was retried.
func TestRunExecution_RecordsRetryCount(t *testing.T) {
	store, _ := newMigratedStore(t)
	ctx := context.Background()
	calls, fails := 0, 2
	reg := NewNodeTypeRegistry()
	reg.Register("test.flaky", func() NodeExecutor { return flakyNode{calls: &calls, fails: &fails} })
	wf := &Workflow{ID: "wf-retry-count", Name: "retry count", Nodes: []WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.flaky", Name: "Flaky", Config: map[string]interface{}{
			"retry_policy": map[string]interface{}{"max_retries": 3, "initial_delay": 0.001},
		}},
	}, Connections: []WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "f", TargetHandle: "main"}}}
	if err := store.CreateWorkflow(ctx, wf); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	exec := &WorkflowExecution{ID: "e-retry-count", WorkflowID: wf.ID, Status: "RUNNING", TriggerType: "manual"}
	if err := store.CreateExecution(ctx, exec); err != nil {
		t.Fatalf("create execution: %v", err)
	}
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunExecution(ctx, exec, wf, dag, reg, store, nil, NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatalf("run: %v", err)
	}
	got, err := store.GetExecution(ctx, exec.ID)
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	for _, n := range got.Nodes {
		if n.NodeID == "f" {
			if n.Status != "SUCCESS" || n.RetryCount != 2 {
				t.Fatalf("Flaky: status %s retry_count %d, want SUCCESS after 2 retries", n.Status, n.RetryCount)
			}
			return
		}
	}
	t.Fatalf("no row for the Flaky node: %+v", got.Nodes)
}
