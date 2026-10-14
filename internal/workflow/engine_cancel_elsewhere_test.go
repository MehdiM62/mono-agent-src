package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/account/accounttest"
)

// TestCancelFromAnotherProcessStopsTheRun: `workflow cancel` only marks the
// row CANCELLED when the daemon owns the run. The engine that runs it must
// stop it, and the run's end must not overwrite CANCELLED.
func TestCancelFromAnotherProcessStopsTheRun(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	eng, store, ids := refusalRun(t, 0, 1)
	ctx := context.Background()

	if _, err := store.RawDB().ExecContext(ctx,
		`UPDATE workflow_executions SET status = 'CANCELLED', finished_at = CURRENT_TIMESTAMP WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	eng.resumeTick(ctx)

	for deadline := time.Now().Add(5 * time.Second); len(eng.queue.RunningIDs()) > 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the run cancelled elsewhere is still running")
		}
	}
	if st, _ := store.GetExecutionStatus(ctx, ids[0]); st != "CANCELLED" {
		t.Fatalf("status = %s, want CANCELLED", st)
	}
}

// TestSetExecutionFinishedKeepsCancelled: a run that ends after another
// process cancelled it keeps CANCELLED.
func TestSetExecutionFinishedKeepsCancelled(t *testing.T) {
	store := newFullEngineStore(t)
	ctx := context.Background()
	exec := &WorkflowExecution{ID: "e1", WorkflowID: "w1", Status: "RUNNING", ProfileID: "default"}
	if err := store.CreateExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RawDB().ExecContext(ctx, `UPDATE workflow_executions SET status = 'CANCELLED' WHERE id = 'e1'`); err != nil {
		t.Fatal(err)
	}
	if err := store.SetExecutionFinished(ctx, "e1", "SUCCESS", ""); err != nil {
		t.Fatal(err)
	}
	if st, _ := store.GetExecutionStatus(ctx, "e1"); st != "CANCELLED" {
		t.Fatalf("status = %s, want CANCELLED", st)
	}
}
