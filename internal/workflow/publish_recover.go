package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// intent is a row of workflow_publications.
type intent struct {
	ID          int64
	WorkflowID  string
	Kind        string
	State       string
	BaseHash    string
	TargetHash  string
	TargetSHA   string
	PrevSHA     string
	PrevBytes   []byte
	PrevExisted bool
	OwnerPID    int
	StartedAt   time.Time
}

const intentColumns = `id, workflow_id, kind, state, base_hash, target_hash, target_sha, prev_sha, prev_bytes, prev_existed, owner_pid, started_at`

func scanIntent(row interface{ Scan(...any) error }) (*intent, error) {
	in := &intent{}
	var existed int
	var started sqliteTime
	err := row.Scan(&in.ID, &in.WorkflowID, &in.Kind, &in.State, &in.BaseHash, &in.TargetHash, &in.TargetSHA,
		&in.PrevSHA, &in.PrevBytes, &existed, &in.OwnerPID, &started)
	in.PrevExisted, in.StartedAt = existed != 0, started.Time
	return in, err
}

// live reports whether the process that opened the intent is still making the change.
func (in *intent) live() bool {
	return in.OwnerPID > 0 && time.Since(in.StartedAt) < publicationLease && processAlive(in.OwnerPID)
}

// quarantineDir is where a file nobody can attribute is copied, beside the workflows folder
// (every *.json inside the folder is read as a workflow).
func (s *WorkflowFileStore) quarantineDir() string {
	return filepath.Join(filepath.Dir(s.dir), "workflow-quarantine")
}

const dropIntentSQL = `DELETE FROM workflow_publications WHERE id = ?`

// resolveIntent makes the file agree with the committed rows again after a publication that did
// not finish, as a function of the intent, the file and the rows. It runs inside tx, which holds
// the write lock. Rolling back, not forward: the process that opened the intent never reported
// success.
func (h *HybridWorkflowStore) resolveIntent(ctx context.Context, tx *sql.Tx, in *intent) (unresolved bool, err error) {
	raw, existed, err := h.files.readRaw(in.WorkflowID)
	if err != nil {
		return false, err
	}
	file := ""
	if existed {
		file = shaHex(append([]byte{}, raw...))
	}
	rowsHash := ""
	rows, err := getWorkflowFrom(ctx, tx, in.WorkflowID)
	switch {
	case err == nil:
		if rowsHash, err = definitionHash(rows); err != nil {
			return false, err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}
	drop := func() error {
		_, err := tx.ExecContext(ctx, dropIntentSQL, in.ID)
		return err
	}
	switch {
	case in.Kind != PubDelete && in.TargetHash != "" && rowsHash == in.TargetHash:
		// The rows already hold the target: make the file say so.
		if file != in.TargetSHA {
			data, err := h.files.renderWorkflow(rows)
			if err != nil {
				return false, err
			}
			if err := h.files.writeRaw(in.WorkflowID, data); err != nil {
				return false, err
			}
		}
		return false, drop()
	case file == in.PrevSHA:
		return false, drop()
	case file == in.TargetSHA:
		if err := failpoint("during-restore"); err != nil {
			return false, err
		}
		if guardedRestoreFault != nil {
			if err := guardedRestoreFault(); err != nil {
				return false, err
			}
		}
		if err := h.files.restoreRaw(in.WorkflowID, in.PrevBytes, in.PrevExisted); err != nil {
			return false, err
		}
		return false, drop()
	}
	// Neither the file before nor the file the change wrote: an edit from outside landed in the
	// window. Keep a copy, leave the file alone, and let a person choose.
	dir := h.files.quarantineDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	name := fmt.Sprintf("%s.%d.json", in.WorkflowID, time.Now().UnixNano())
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE workflow_publications SET state = 'unresolved', note = ? WHERE id = ?`,
		"the file changed during the change; a copy is in "+dir, in.ID)
	return true, err
}

// resolveByID resolves one intent under the write lock. Without force an intent whose owner is
// still working is left alone.
func (h *HybridWorkflowStore) resolveByID(ctx context.Context, id int64, force bool) error {
	tx, done, err := h.beginDurable(ctx)
	if err != nil {
		return err
	}
	defer done()
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_publications SET state = state WHERE id = ?`, id); err != nil {
		return err
	}
	in, err := scanIntent(tx.QueryRowContext(ctx, `SELECT `+intentColumns+` FROM workflow_publications WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if in.State != "pending" || (!force && in.live()) {
		return nil
	}
	unresolved, err := h.resolveIntent(ctx, tx, in)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if unresolved && force {
		return fmt.Errorf("the file changed while the change was in progress; a copy was kept")
	}
	return nil
}

func (h *HybridWorkflowStore) rollbackIntent(ctx context.Context, id int64) error {
	return h.resolveByID(ctx, id, true)
}

// pendingIntents lists the ids of unfinished publications, optionally of one workflow.
func (h *HybridWorkflowStore) pendingIntents(ctx context.Context, workflowID string) ([]int64, error) {
	q, args := `SELECT id FROM workflow_publications WHERE state = 'pending'`, []any(nil)
	if workflowID != "" {
		q, args = q+` AND workflow_id = ?`, []any{workflowID}
	}
	rows, err := h.sql.db.QueryContext(ctx, q, args...)
	if err != nil {
		if isNoTable(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RecoverPublications resolves every publication whose owner is gone and returns how many it
// looked at. Anyone may run it; a no-op query when nothing is open.
func (h *HybridWorkflowStore) RecoverPublications(ctx context.Context) (int, error) {
	if h.files == nil {
		return 0, nil
	}
	ids, err := h.pendingIntents(ctx, "")
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, id := range ids {
		if err := h.resolveByID(ctx, id, false); err != nil {
			errs = append(errs, err)
		}
	}
	return len(ids), errors.Join(errs...)
}

func (h *HybridWorkflowStore) recoverID(ctx context.Context, workflowID string) error {
	ids, err := h.pendingIntents(ctx, workflowID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := h.resolveByID(ctx, id, false); err != nil {
			return err
		}
	}
	return nil
}

// openIntents is the set of workflow ids with a publication in flight or unresolved, after
// resolving the abandoned ones: for these the committed rows, not the file, are the definition.
func (h *HybridWorkflowStore) openIntents(ctx context.Context) map[string]bool {
	if h.files == nil {
		return nil
	}
	read := func() map[string]bool {
		rows, err := h.sql.db.QueryContext(ctx, `SELECT workflow_id FROM workflow_publications`)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var out map[string]bool
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				if out == nil {
					out = map[string]bool{}
				}
				out[id] = true
			}
		}
		return out
	}
	open := read()
	if len(open) == 0 {
		return nil
	}
	if n, _ := h.RecoverPublications(ctx); n > 0 {
		open = read()
	}
	return open
}
