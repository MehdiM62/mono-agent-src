package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNodeState_ContextRoundTrip(t *testing.T) {
	if got := NodeStateFromContext(context.Background()); got != nil {
		t.Errorf("no slot: got %s, want nil", got)
	}
	if err := SetNodeState(context.Background(), 1); !errors.Is(err, ErrNodeStateUnavailable) {
		t.Errorf("no slot: err = %v, want ErrNodeStateUnavailable", err)
	}
	ctx := WithNodeState(context.Background(), json.RawMessage(`{"a":1}`))
	if got := string(NodeStateFromContext(ctx)); got != `{"a":1}` {
		t.Errorf("initial = %s", got)
	}
	if err := SetNodeState(ctx, map[string]int{"b": 2}); err != nil {
		t.Fatal(err)
	}
	if got := string(NodeStateFromContext(ctx)); got != `{"b":2}` {
		t.Errorf("after set = %s", got)
	}
	if err := SetNodeState(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got := NodeStateFromContext(ctx); got != nil {
		t.Errorf("after clear = %s, want nil", got)
	}
}

// statefulPauser pauses on its first run after saving state, and on the
// re-run (resume) reports the state it finds.
type statefulPauser struct{ seen *string }

func (statefulPauser) Type() string { return "test.stateful" }
func (s statefulPauser) Execute(ctx context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	if st := NodeStateFromContext(ctx); st != nil {
		*s.seen = string(st)
		_ = SetNodeState(ctx, nil)
		return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
	}
	if err := SetNodeState(ctx, map[string]string{"until": "later"}); err != nil {
		return nil, err
	}
	return nil, PauseFor(time.Hour, "test")
}

// A node's state saved before a pause is handed back to it on resume.
func TestNodeState_SurvivesPauseResume(t *testing.T) {
	var seen string
	reg := NewNodeTypeRegistry()
	reg.Register("test.stateful", func() NodeExecutor { return statefulPauser{seen: &seen} })
	wf := &Workflow{ID: "wf", Nodes: []WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "w", Type: "test.stateful", Name: "W"},
	}, Connections: []WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "w"}}}
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	store := &stubStore{}
	exec := &WorkflowExecution{ID: "e", WorkflowID: wf.ID}
	if err := RunExecution(context.Background(), exec, wf, dag, reg, store, nil, NewExpressionEngine(), zerolog.Nop()); !errors.Is(err, ErrExecutionPaused) {
		t.Fatalf("err = %v, want paused", err)
	}
	exec.ResumeState = store.waitingState
	if err := RunExecution(context.Background(), exec, wf, dag, reg, store, nil, NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if seen != `{"until":"later"}` {
		t.Errorf("state on resume = %q", seen)
	}
}
