package workflow

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

type finished struct{ id, status, class string }

// installFinishHook sets OnExecutionFinished for the test and returns the
// channel it reports on; the hook reads error_class to prove it runs after
// the final status is stored.
func installFinishHook(t *testing.T) <-chan finished {
	t.Helper()
	ch := make(chan finished, 8)
	prev := OnExecutionFinished
	OnExecutionFinished = func(ctx context.Context, db *sql.DB, id, status string) {
		var class sql.NullString
		_ = db.QueryRowContext(ctx, `SELECT error_class FROM workflow_executions WHERE id = ?`, id).Scan(&class)
		ch <- finished{id, status, class.String}
	}
	t.Cleanup(func() { OnExecutionFinished = prev })
	return ch
}

func waitFinished(t *testing.T, ch <-chan finished) finished {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(10 * time.Second):
		t.Fatal("OnExecutionFinished was not called")
	}
	return finished{}
}

func TestOnExecutionFinishedFailedRun(t *testing.T) {
	ch := installFinishHook(t)
	r := newErrorWorkflowRig(t, errors.New("http error: 502 Bad Gateway"))
	r.addWorkflow(t, "wf-main", "trigger.manual", "test.fail", nil, true)
	execID, err := r.eng.TriggerWorkflow(context.Background(), "wf-main", nil)
	if err != nil {
		t.Fatal(err)
	}
	f := waitFinished(t, ch)
	if f.id != execID || f.status != "FAILED" {
		t.Fatalf("hook got %+v, want %s FAILED", f, execID)
	}
	if f.class == "" {
		t.Errorf("error_class not stored before the hook ran")
	}
}

func TestOnExecutionFinishedSuccessAndPanic(t *testing.T) {
	ch := installFinishHook(t)
	r := newErrorWorkflowRig(t, nil)
	r.addWorkflow(t, "wf-ok", "trigger.manual", "test.capture", nil, true)
	execID, _ := r.eng.TriggerWorkflow(context.Background(), "wf-ok", nil)
	if f := waitFinished(t, ch); f.id != execID || f.status != "SUCCESS" {
		t.Fatalf("hook got %+v, want %s SUCCESS", f, execID)
	}

	// A panicking hook must not break the next run.
	OnExecutionFinished = func(context.Context, *sql.DB, string, string) { panic("boom") }
	execID, _ = r.eng.TriggerWorkflow(context.Background(), "wf-ok", nil)
	if st := waitForTerminalStatus(t, r.store, execID, 10*time.Second); st != "SUCCESS" {
		t.Fatalf("status = %s after a panicking hook", st)
	}
}

func TestNotifyExecutionFinishedNilSafe(t *testing.T) {
	prev := OnExecutionFinished
	OnExecutionFinished = nil
	t.Cleanup(func() { OnExecutionFinished = prev })
	notifyExecutionFinished(nil, "x", "SUCCESS") // no hook, no db: no panic
}
