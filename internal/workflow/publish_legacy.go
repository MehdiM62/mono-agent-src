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
		return true, nil // an unreadable marker fails closed: nothing is grandfathered
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

// ensureClaimed records the legacy set the first time this store is used for files, in whatever
// process that is (a CLI command, the daemon, the MCP server, the API): not only the CLI's sync.
func (h *HybridWorkflowStore) ensureClaimed(ctx context.Context) {
	if h.files == nil {
		return
	}
	h.claimMu.Lock()
	defer h.claimMu.Unlock()
	if h.claimed {
		return
	}
	if err := h.ClaimLegacyFiles(ctx); err == nil {
		h.claimed = true
	}
}

// pruneLegacy takes a workflow out of the legacy set when it is removed, so a file later recreated
// under the same id is a new file, not a grandfathered one.
func pruneLegacy(ctx context.Context, tx *sql.Tx, id string) error {
	var raw string
	switch err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, legacyFilesKey).Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows) || isNoTable(err):
		return nil
	case err != nil:
		return err
	}
	var ids []string
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return nil
	}
	kept := ids[:0]
	for _, l := range ids {
		if l != id {
			kept = append(kept, l)
		}
	}
	if len(kept) == len(ids) {
		return nil
	}
	out, _ := json.Marshal(kept)
	_, err := tx.ExecContext(ctx, `UPDATE settings SET value = ? WHERE key = ?`, string(out), legacyFilesKey)
	return err
}

// AdoptableFiles lists workflow files that appeared after the legacy set was recorded and have no
// rows: they are neither imported nor served until an operator adopts them.
func (h *HybridWorkflowStore) AdoptableFiles(ctx context.Context) ([]string, error) {
	if h.files == nil {
		return nil, nil
	}
	entries, err := os.ReadDir(h.files.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !ok || strings.HasSuffix(id, ".ai-backup") {
			continue
		}
		var n int
		if err := h.sql.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflows WHERE id = ?`, id).Scan(&n); err != nil || n > 0 {
			continue
		}
		if post, err := postMarkerFile(ctx, h.sql.db, id); err != nil || !post {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

// legacyMarker returns the recorded legacy set. recorded is false before the first claim; an
// unreadable marker counts as recorded and empty (fail closed).
func legacyMarker(ctx context.Context, q rowQuerier) (ids map[string]bool, recorded bool, err error) {
	var raw string
	switch err := q.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, legacyFilesKey).Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil
	case err != nil:
		if isNoTable(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var list []string
	if json.Unmarshal([]byte(raw), &list) != nil {
		return map[string]bool{}, true, nil
	}
	ids = make(map[string]bool, len(list))
	for _, l := range list {
		ids[l] = true
	}
	return ids, true, nil
}
