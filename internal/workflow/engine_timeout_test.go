package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// blockNode runs until the run's context ends.
type blockNode struct{}

func (blockNode) Type() string { return "test.block" }
func (blockNode) Execute(ctx context.Context, _ NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A run its executionTimeout ends is CANCELLED with the timeout message and
// class timeout, and starts no error workflow.
func TestEngineExecutionTimeoutEndsCancelled(t *testing.T) {
	r := newErrorWorkflowRig(t, nil)
	r.eng.registry.Register("test.block", func() NodeExecutor { return blockNode{} })
	r.addWorkflow(t, "wf-err", TriggerNodeTypeError, "test.capture", nil, false)
	r.addWorkflow(t, "wf-main", "trigger.manual", "test.block", map[string]interface{}{
		ErrorWorkflowSetting: "wf-err", ExecutionTimeoutSetting: 0.3,
	}, true)
	execID, err := r.eng.TriggerWorkflow(context.Background(), "wf-main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st := waitForTerminalStatus(t, r.store, execID, 10*time.Second); st != "CANCELLED" {
		t.Fatalf("status = %s, want CANCELLED", st)
	}
	ex, _ := r.store.GetExecution(context.Background(), execID)
	if ex.ErrorMessage != "execution timeout after 0.3s" {
		t.Errorf("error_message = %q", ex.ErrorMessage)
	}
	if ex.ErrorClass != string(ClassTimeout) {
		t.Errorf("error_class = %q, want timeout", ex.ErrorClass)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(r.errorRuns(t, "wf-err")); n != 0 {
		t.Fatalf("a timed-out run started %d error workflow runs", n)
	}
}

// classAtFinishStore records the execution's error_class at the moment its
// terminal status is written.
type classAtFinishStore struct {
	*SQLiteWorkflowStore
	class string
}

func (s *classAtFinishStore) SetExecutionFinished(ctx context.Context, id, status, errMsg string) error {
	_ = s.db.QueryRow(`SELECT error_class FROM workflow_executions WHERE id = ?`, id).Scan(&s.class)
	return s.SQLiteWorkflowStore.SetExecutionFinished(ctx, id, status, errMsg)
}

// The class of a finished run is in place before its terminal status is
// written, so a reader that sees CANCELLED never sees the interrupted node's
// class (the flake TestEngineExecutionTimeoutEndsCancelled hit).
func TestTimeoutClassWrittenBeforeTerminalStatus(t *testing.T) {
	store := &classAtFinishStore{SQLiteWorkflowStore: newFullEngineStore(t)}
	eng := NewWorkflowEngineWithStore(store, store.RawDB(), nil, NewNodeTypeRegistry(), EngineConfig{ProfileID: "default"}, zerolog.Nop())
	t.Cleanup(func() { _ = eng.Stop() })
	if _, err := store.db.Exec(`INSERT INTO workflow_executions (id, workflow_id, status, trigger_type, error_class)
		VALUES ('e1', 'wf', 'RUNNING', 'trigger.manual', 'cancelled')`); err != nil {
		t.Fatal(err)
	}
	eng.persistExecutionFinished(zerolog.Nop(), "e1", "CANCELLED", "execution timeout after 0.3s")
	if store.class != string(ClassTimeout) {
		t.Fatalf("error_class when the status was written = %q, want timeout", store.class)
	}
}
