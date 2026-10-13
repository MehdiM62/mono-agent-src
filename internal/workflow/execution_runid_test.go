package workflow

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/logging"
)

// runIDNode records the run id its context carries.
type runIDNode struct{ got *string }

func (n runIDNode) Type() string { return "test.runid" }
func (n runIDNode) Execute(ctx context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	*n.got = logging.RunID(ctx)
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// The execution id is the run's run_id: nodes see it in their context (so
// the child processes they start get MONOAGENT_RUN_ID via logging.ChildEnv)
// and the run's own log lines carry it.
func TestRunExecution_RunIDIsTheExecutionID(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	var got string

	reg := NewNodeTypeRegistry()
	reg.Register("test.runid", func() NodeExecutor { return runIDNode{&got} })
	reg.Register("test.count", func() NodeExecutor { return countNode{new(int)} })

	wf := &Workflow{
		ID:   "wf-runid",
		Name: "runid",
		Nodes: []WorkflowNode{
			{ID: "t", WorkflowID: "wf-runid", Type: "trigger.manual", Name: "T"},
			{ID: "r", WorkflowID: "wf-runid", Type: "test.runid", Name: "R"},
			// A credential miss makes the run log a line, which must carry the run_id.
			{ID: "n", WorkflowID: "wf-runid", Type: "test.count", Name: "N", Config: map[string]interface{}{"credential_id": "wc_missing"}},
		},
		Connections: []WorkflowConnection{
			{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "r", TargetHandle: "main"},
			{SourceNodeID: "r", SourceHandle: "main", TargetNodeID: "n", TargetHandle: "main"},
		},
	}
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
	exec := &WorkflowExecution{ID: "e-runid-1", WorkflowID: wf.ID, TriggerNodeID: "t"}
	if err := RunExecution(context.Background(), exec, wf, dag, reg, &stubStore{}, nil, NewExpressionEngine(), logger); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if got != "e-runid-1" {
		t.Errorf("node saw run id %q, want the execution id e-runid-1", got)
	}
	if !strings.Contains(buf.String(), `"run_id":"e-runid-1"`) {
		t.Errorf("the run's log lines should carry run_id, got: %s", buf.String())
	}
}
