package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

const (
	legacyFilesKey = "workflow_files.legacy_set"

	// ImportNewFileNote is the note of the first version of a workflow that appeared as a file after
	// the legacy set was recorded: it starts inactive, and an operator has to activate it.
	ImportNewFileNote = "imported from an outside file, inactive"
)

// ClaimLegacyFiles records, once and atomically, which workflow files exist now. Those are the
// workflows older versions saved as files; a file-only workflow that appears later (dropped in by
// a person, a script or an agent) is imported inactive, whatever its is_active says. The list is
// written under the write lock by whichever process gets there first, so every process agrees on
// it. Before the claim, every file counts as legacy (nothing has been synced yet).
func (h *HybridWorkflowStore) ClaimLegacyFiles(ctx context.Context) error {
	if h.files == nil {
		return nil
	}
	tx, err := h.sql.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A write first: the claim and the directory listing happen under the write lock.
	if _, err := tx.ExecContext(ctx, `UPDATE settings SET value = value WHERE key = ?`, legacyFilesKey); err != nil {
		return err
	}
	var have string
	switch err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, legacyFilesKey).Scan(&have); {
	case err == nil:
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	entries, err := os.ReadDir(h.files.dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	ids := []string{}
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() && !strings.HasSuffix(id, ".ai-backup") {
			ids = append(ids, id)
		}
	}
	raw, _ := json.Marshal(ids)
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)`, legacyFilesKey, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}

// postMarkerFile reports whether the legacy set was recorded and this workflow's file is not in it.
func postMarkerFile(ctx context.Context, q rowQuerier, id string) (bool, error) {
	var raw string
	switch err := q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, legacyFilesKey).Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		if isNoTable(err) {
			return false, nil
		}
		return false, err
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return false, nil // an unreadable marker is treated as not recorded: today's behaviour
	}
	for _, l := range ids {
		if l == id {
			return false, nil
		}
	}
	return true, nil
}

// ImportedInactive lists workflows that appeared as outside files after the legacy set was
// recorded and are still inactive, for `doctor`.
func (h *HybridWorkflowStore) ImportedInactive(ctx context.Context) ([]string, error) {
	rows, err := h.sql.db.QueryContext(ctx, `
		SELECT DISTINCT v.workflow_id FROM workflow_versions v JOIN workflows w ON w.id = v.workflow_id
		WHERE v.note = ? AND w.is_active = 0 ORDER BY v.workflow_id`, ImportNewFileNote)
	if err != nil {
		if isNoTable(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
