package workflow

import (
	"context"
	"os"
)

// queuedStarter is the guarded QUEUED → RUNNING transition
// (SQLiteWorkflowStore.StartQueuedExecution). Stores without it — test
// stubs — fall back to the unguarded SetExecutionStarted.
type queuedStarter interface {
	StartQueuedExecution(ctx context.Context, id string) (bool, error)
}

// startQueued claims executionID for this dispatch. False means the row is
// no longer QUEUED for this process and the caller must not run it.
func (e *WorkflowEngine) startQueued(ctx context.Context, executionID string) (bool, error) {
	if qs, ok := e.store.(queuedStarter); ok {
		return qs.StartQueuedExecution(ctx, executionID)
	}
	if st, err := e.store.GetExecutionStatus(ctx, executionID); err == nil && st == "CANCELLED" {
		return false, nil
	}
	return true, e.store.SetExecutionStarted(ctx, executionID)
}

// ownerPID is the pid stamped on an execution this process enqueues itself:
// a live owner keeps other engines (and its own adoption sweep) from adopting
// the row while it waits in the queue for a concurrency slot.
func ownerPID() int { return os.Getpid() }
