package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrChangedSincePreview: the workflow is not the one the caller reviewed.
var ErrChangedSincePreview = errors.New("the workflow changed since it was previewed")

// ReviewHash identifies what a person looked at before agreeing to an edit: the
// definition the approval code hashes (name, description, settings, nodes,
// connections) together with whether the workflow is active. Two reads of the
// same stored workflow hash the same, from either store.
func ReviewHash(w *Workflow) (string, error) {
	def, err := definitionHash(w)
	if err != nil {
		return "", err
	}
	state := "inactive"
	if w.IsActive {
		state = "active"
	}
	sum := sha256.Sum256([]byte(def + "|" + state))
	return hex.EncodeToString(sum[:]), nil
}

// SaveGuard conditions SaveNodesGuarded.
type SaveGuard struct {
	// ExpectHash is the ReviewHash the stored workflow must still have.
	ExpectHash string
	// RevokeApprovals removes every stored approval of the workflow in the same
	// transaction, so the result needs a fresh one even when its definition
	// equals an older approved one.
	RevokeApprovals bool
}

// SaveNodesGuarded is SaveWorkflowNodes as one transaction that first takes the
// database's write lock, then compares the stored workflow's ReviewHash with
// guard.ExpectHash, and only then writes (and revokes approvals when asked). A
// second writer waits for the lock and then finds a different hash:
// ErrChangedSincePreview. The canonical workflow file is synced afterwards, as
// SaveWorkflowNodes does.
func (h *HybridWorkflowStore) SaveNodesGuarded(ctx context.Context, workflowID string, nodes []WorkflowNode, guard SaveGuard) error {
	h.ensureSQLWorkflow(ctx, workflowID)
	tx, err := h.sql.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning guarded save: %w", err)
	}
	defer tx.Rollback()
	// A write first: this connection now holds the write lock, so nothing can
	// change the workflow between the comparison below and the commit.
	res, err := tx.ExecContext(ctx, `UPDATE workflows SET updated_at = updated_at WHERE id = ?`, workflowID)
	if err != nil {
		return fmt.Errorf("locking workflow %s: %w", workflowID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("workflow %s not found", workflowID)
	}
	cur, err := getWorkflowFrom(ctx, tx, workflowID)
	if err != nil {
		return err
	}
	have, err := ReviewHash(cur)
	if err != nil {
		return err
	}
	if have != guard.ExpectHash {
		return ErrChangedSincePreview
	}
	if err := saveNodesTx(ctx, tx, workflowID, nodes); err != nil {
		return err
	}
	if guard.RevokeApprovals {
		if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_approvals WHERE workflow_id = ?`, workflowID); err != nil && !isNoTable(err) {
			return fmt.Errorf("revoking approvals: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing guarded save: %w", err)
	}
	h.syncFileNodes(ctx, workflowID)
	_, _ = h.captureVersion(ctx, workflowID)
	return h.saved(ctx, workflowID, nil)
}

// RevokeApprovals removes every stored approval of workflow id.
func (h *HybridWorkflowStore) RevokeApprovals(ctx context.Context, workflowID string) error {
	_, err := h.sql.db.ExecContext(ctx, `DELETE FROM workflow_approvals WHERE workflow_id = ?`, workflowID)
	if err != nil && !isNoTable(err) {
		return err
	}
	return nil
}

func isNoTable(err error) bool { return err != nil && strings.Contains(err.Error(), "no such table") }

// getWorkflowFrom is SQLiteWorkflowStore.GetWorkflow over a transaction.
func getWorkflowFrom(ctx context.Context, q rowQuerier, id string) (*Workflow, error) {
	w := &Workflow{}
	var isActive int
	var createdAt, updatedAt sqliteTime
	err := q.QueryRowContext(ctx, `
		SELECT id, name, description, is_active, version, profile_id, created_at, updated_at
		FROM workflows WHERE id = ?`, id,
	).Scan(&w.ID, &w.Name, &w.Description, &isActive, &w.Version, &w.ProfileID, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("getting workflow %s: %w", id, err)
	}
	w.CreatedAt, w.UpdatedAt = createdAt.Time, updatedAt.Time
	w.IsActive = isActive != 0
	loadSettingsFrom(ctx, q, w)
	if w.Nodes, err = loadNodesFrom(ctx, q, id); err != nil {
		return nil, err
	}
	if w.Connections, err = loadConnectionsFrom(ctx, q, id); err != nil {
		return nil, err
	}
	return w, nil
}
