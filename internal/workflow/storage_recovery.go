package workflow

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// ListAdoptableExecutions returns the IDs of QUEUED executions that have no
// owner (pid 0/NULL) and no resume_state. These are the rows
// `workflow run --no-wait` persists before exiting, waiting for any live
// engine to claim them; crash-resume residue (QUEUED with resume_state) is
// excluded — RecoverStaleExecutions flips those back to WAITING for the
// resume loop instead.
//
// A plain QUEUED row whose owner process has died never started (the owner
// flips it to RUNNING before running anything), so it is released back to
// pid 0 here and listed too: running it is its first and only run.
func (s *SQLiteWorkflowStore) ListAdoptableExecutions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(pid, 0) FROM workflow_executions
		WHERE status = 'QUEUED'
		  AND COALESCE(resume_state, '') = ''`)
	if err != nil {
		return nil, fmt.Errorf("listing adoptable executions: %w", err)
	}
	type row struct {
		id  string
		pid int
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.pid); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning adoptable execution: %w", err)
		}
		all = append(all, r)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("listing adoptable executions: %w", err)
	}
	self := os.Getpid()
	var ids []string
	for _, r := range all {
		if r.pid != 0 {
			if r.pid == self || processAlive(r.pid) {
				continue
			}
			released, err := s.releaseDeadOwner(ctx, r.id, r.pid)
			if err != nil {
				return nil, err
			}
			if !released {
				continue
			}
		}
		ids = append(ids, r.id)
	}
	return ids, nil
}

// releaseDeadOwner hands a dead process's never-started QUEUED row back to
// pid 0, guarded on the pid it was read with so a concurrent claim wins.
func (s *SQLiteWorkflowStore) releaseDeadOwner(ctx context.Context, id string, deadPID int) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflow_executions SET pid = 0
		 WHERE id = ? AND status = 'QUEUED' AND pid = ? AND COALESCE(resume_state, '') = ''`,
		id, deadPID)
	if err != nil {
		return false, fmt.Errorf("releasing queued execution %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClaimQueuedExecution atomically claims an unowned QUEUED execution for this
// process: a CAS on pid 0/NULL → self. Among engines sharing the DB exactly
// one wins the claim; losers get false. The status stays QUEUED —
// SetExecutionStarted flips it to RUNNING (and re-stamps pid) once the run
// actually dispatches.
func (s *SQLiteWorkflowStore) ClaimQueuedExecution(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflow_executions SET pid = ? WHERE id = ? AND status = 'QUEUED' AND (pid = 0 OR pid IS NULL)`,
		os.Getpid(), id)
	if err != nil {
		return false, fmt.Errorf("claiming queued execution %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---------------------------------------------------------------------------
// Recovery & maintenance
// ---------------------------------------------------------------------------

// RecoverStaleExecutions transitions stale RUNNING or QUEUED executions to
// FAILED. This should be called once on process startup to handle executions
// that were interrupted by a previous crash or restart.
//
// QUEUED rows of a dead (or no) owner never started, so they are not failed:
//   - with a persisted resume_state it is the residue of a crash between the
//     resume CAS (WAITING → QUEUED) and the run — flipped back to WAITING
//     so the resume loop re-enqueues the approved Human-in-Loop run;
//   - without one it is a row awaiting adoption (`workflow run --no-wait`
//     persists one and exits; a process that exits with runs still waiting
//     for a slot leaves its own). It is left QUEUED with pid 0 for a live
//     engine to claim — failing it here would destroy a run nobody started.
func (s *SQLiteWorkflowStore) RecoverStaleExecutions(ctx context.Context) error {
	// Only fail executions whose owning process is no longer alive. A concurrent
	// engine (e.g. the `daemon` process) may legitimately have RUNNING executions;
	// blindly failing all RUNNING/QUEUED rows corrupts its in-flight state. A
	// null/zero/dead pid means the row has no live owner and is safe to recover.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, status, COALESCE(pid, 0), COALESCE(resume_state, '') FROM workflow_executions WHERE status IN ('RUNNING', 'QUEUED')`)
	if err != nil {
		return fmt.Errorf("recovering stale executions: %w", err)
	}
	defer rows.Close()

	self := os.Getpid()
	var staleIDs []string
	var resumableIDs []string
	var releaseIDs []string
	for rows.Next() {
		var id, status, resumeState string
		var pid int
		if err := rows.Scan(&id, &status, &pid, &resumeState); err != nil {
			return fmt.Errorf("recovering stale executions: scan: %w", err)
		}
		if pid == self {
			continue // never fail our own in-flight rows
		}
		if !processAlive(pid) {
			if status == "QUEUED" {
				if resumeState != "" {
					// Crashed between resume-CAS and the run: flip back to
					// WAITING so the resume loop re-enqueues the approved run.
					resumableIDs = append(resumableIDs, id)
				} else if pid != 0 {
					// Never started: release it for adoption.
					releaseIDs = append(releaseIDs, id)
				}
				// else: unowned --no-wait row — an adoption candidate, not
				// stale. Leave it QUEUED for a live engine to claim.
				continue
			}
			staleIDs = append(staleIDs, id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("recovering stale executions: %w", err)
	}

	for _, id := range resumableIDs {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE workflow_executions
			SET status = 'WAITING'
			WHERE id = ? AND status = 'QUEUED'`, id); err != nil {
			return fmt.Errorf("recovering resumable execution %s: %w", id, err)
		}
	}

	for _, id := range releaseIDs {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE workflow_executions
			SET pid = 0
			WHERE id = ? AND status = 'QUEUED' AND COALESCE(resume_state, '') = ''`, id); err != nil {
			return fmt.Errorf("releasing queued execution %s: %w", id, err)
		}
	}

	for _, id := range staleIDs {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE workflow_executions
			SET status = 'FAILED',
			    error_message = 'recovered: process restart',
			    finished_at = CURRENT_TIMESTAMP,
			    resume_state = ''
			WHERE id = ? AND status IN ('RUNNING', 'QUEUED')`, id); err != nil {
			return fmt.Errorf("recovering stale execution %s: %w", id, err)
		}
	}
	return nil
}

// ReapStaleRunningExecutions fails executions that have been RUNNING longer
// than olderThan and whose owning process is dead (or is this process, which
// cannot legitimately have a run that old). A crashed daemon can leave such
// rows behind when RecoverStaleExecutions missed them (e.g. the pid was
// recycled); this periodic sweep keeps them from masquerading as active runs.
func (s *SQLiteWorkflowStore) ReapStaleRunningExecutions(ctx context.Context, olderThan time.Time) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, COALESCE(pid, 0) FROM workflow_executions
		 WHERE status = 'RUNNING' AND started_at IS NOT NULL AND started_at < ?`,
		olderThan,
	)
	if err != nil {
		return fmt.Errorf("reaping stale running executions: %w", err)
	}
	defer rows.Close()

	self := os.Getpid()
	var ids []string
	for rows.Next() {
		var id string
		var pid int
		if err := rows.Scan(&id, &pid); err != nil {
			return fmt.Errorf("reaping stale running executions: scan: %w", err)
		}
		if pid <= 0 {
			continue // unknown owner — can't prove it's dead, stay conservative
		}
		if pid == self || !processAlive(pid) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reaping stale running executions: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, olderThan)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query := `UPDATE workflow_executions
		SET status = 'FAILED',
		    error_message = 'stale: running for over 24h',
		    finished_at = CURRENT_TIMESTAMP,
		    resume_state = ''
		WHERE status = 'RUNNING' AND started_at < ? AND id IN (` +
		strings.Join(placeholders, ",") + `)`
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("reaping stale running executions: %w", err)
	}
	return nil
}

// CancelQueuedExecution marks a not-yet-dispatched (QUEUED) execution CANCELLED
// so it will not run when a worker later picks it up. Returns true if a queued
// row was actually transitioned. Running executions are unaffected (they are
// stopped via context cancellation instead).
func (s *SQLiteWorkflowStore) CancelQueuedExecution(ctx context.Context, id string) (bool, error) {
	// Covers QUEUED (not yet dispatched) and WAITING (paused at a HIL node with
	// no live goroutine) — both are cancellable purely via a status flip.
	// Clearing resume_state ensures a cancelled paused run can never be resumed.
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflow_executions SET status = 'CANCELLED', finished_at = CURRENT_TIMESTAMP, resume_state = '' WHERE id = ? AND status IN ('QUEUED','WAITING')`, id)
	if err != nil {
		return false, fmt.Errorf("cancelling queued execution %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ResumeWaitingExecution atomically flips a WAITING execution to QUEUED, so that
// among concurrent engines sharing the DB exactly one wins the resume (the
// others get false). The winner becomes the owner (pid), which is what lets
// its dispatch pass StartQueuedExecution. Returns whether this caller flipped it.
func (s *SQLiteWorkflowStore) ResumeWaitingExecution(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE workflow_executions SET status = 'QUEUED', pid = ? WHERE id = ? AND status = 'WAITING'`, os.Getpid(), id)
	if err != nil {
		return false, fmt.Errorf("resuming waiting execution %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// PruneExecutions deletes the oldest executions for a workflow when the total
// count exceeds keepCount. Executions are ordered by created_at; the oldest
// are removed first.
//
// WAITING (Human-in-Loop paused) executions are never prune candidates —
// deleting one would destroy a paused run that is still awaiting approval.
// In the same pass, hil_pending rows are cleaned up: rows whose execution no
// longer exists (orphans — the table has no FK), and rows belonging to the
// executions being pruned.
func (s *SQLiteWorkflowStore) PruneExecutions(ctx context.Context, workflowID string, keepCount int) error {
	if keepCount < 0 {
		keepCount = 0
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pruning executions for workflow %s: %w", workflowID, err)
	}

	// hil_pending rows referencing no existing execution (orphans).
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM hil_pending WHERE execution_id NOT IN (SELECT id FROM workflow_executions)`); err != nil {
		tx.Rollback()
		return fmt.Errorf("pruning executions for workflow %s: %w", workflowID, err)
	}

	candidates := `
		SELECT id FROM workflow_executions
		WHERE workflow_id = ?
		  AND status != 'WAITING'
		  AND id NOT IN (
		      SELECT id FROM workflow_executions
		      WHERE workflow_id = ?
		      ORDER BY created_at DESC
		      LIMIT ?
		  )`

	// hil_pending rows of executions about to be pruned.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM hil_pending WHERE execution_id IN (`+candidates+`)`,
		workflowID, workflowID, keepCount); err != nil {
		tx.Rollback()
		return fmt.Errorf("pruning executions for workflow %s: %w", workflowID, err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM workflow_executions WHERE id IN (`+candidates+`)`,
		workflowID, workflowID, keepCount); err != nil {
		tx.Rollback()
		return fmt.Errorf("pruning executions for workflow %s: %w", workflowID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("pruning executions for workflow %s: %w", workflowID, err)
	}
	return nil
}
