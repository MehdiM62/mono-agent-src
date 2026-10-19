package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A parent waiting only for its sub-workflow is in flight: this server's
// engine runs the child and resumes the parent, so workflow_run's wait goes
// on to its timeout and the run counts as timed out, not as paused for a
// person. It used to return at the first poll. A pending review still ends
// the wait at once.
func TestWorkflowRunWaitGoesOnThroughASubWorkflow(t *testing.T) {
	s := newTestServer(t)
	rt, err := s.runtime()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := rt.store.CreateWorkflow(ctx, &workflow.Workflow{ID: "wf-parent", Name: "Parent", ProfileID: "default"}); err != nil {
		t.Fatal(err)
	}
	exec := &workflow.WorkflowExecution{ID: "exec-parent", WorkflowID: "wf-parent", ProfileID: "default", Status: "QUEUED", TriggerType: "manual"}
	if err := rt.store.CreateExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}
	if err := rt.store.SetExecutionWaiting(ctx, exec.ID, `{"completed_nodes":{"t":true}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.db.DB.Exec(`INSERT INTO subworkflow_runs (parent_execution_id, parent_node_id, child_execution_id, item_index, wait)
		VALUES ('exec-parent', 'call', 'child-1', -1, 1)`); err != nil {
		t.Fatal(err)
	}

	const timeout = 1500 * time.Millisecond
	start := time.Now()
	got := waitForExecution(ctx, rt, exec.ID, timeout)
	if waited := time.Since(start); waited < timeout || got.Status != "WAITING" {
		t.Fatalf("returned %s after %v, want it to wait %v", got.Status, waited, timeout)
	}
	if inFlight, err := workflow.RecordInFlight(ctx, rt.store, got); err != nil || !inFlight {
		t.Errorf("the run at the timeout: in flight %v (%v), want true (timed_out)", inFlight, err)
	}

	if _, err := rt.db.DB.Exec(`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, profile_id)
		VALUES ('h1', 'exec-parent', 'wf-parent', 'n', 'Review', 'pending', 'default')`); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	if got := waitForExecution(ctx, rt, exec.ID, 10*time.Second); got.Status != "WAITING" || time.Since(start) > 5*time.Second {
		t.Errorf("a pending review: %s after %v, want WAITING at once", got.Status, time.Since(start))
	}
}
