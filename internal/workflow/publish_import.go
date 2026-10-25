package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

// ImportNote is the note of the version an outside edit is recorded as.
const ImportNote = "edited outside the app"

// ImportResult says what ImportFile did.
type ImportResult struct {
	// Imported: the rows now follow the file.
	Imported bool
	// Created: the workflow had no rows before.
	Created bool
	// Workflow is the imported definition.
	Workflow *Workflow
}

// ImportFile makes the committed rows follow the workflow's file when the file was changed outside
// the store (an editor, a script). It is one transaction: the write lock is held from the look at
// the open changes to the commit, so it cannot interleave with a save (a save records its intent
// before it touches the file, and the importer refuses to read a file whose change is open). The
// file is never rewritten. An edit that does not change the definition only refreshes the file's
// recorded bytes; an edit that does is recorded as a version, and the new definition counts as
// approved only if it equals an approved one (approvals are keyed by definition hash).
//
// Nothing is imported, and nil is returned, when the file is absent, does not parse, was changed
// while it was read, has no profile, accept (when given) refuses it, or its workflow has a change
// open. A definition that shares
// node or connection ids with another workflow returns ErrSharedIDs.
func (h *HybridWorkflowStore) ImportFile(ctx context.Context, id string, accept func(*Workflow) bool) (ImportResult, error) {
	var none ImportResult
	if h.files == nil {
		return none, nil
	}
	tx, err := h.sql.db.BeginTx(ctx, nil)
	if err != nil {
		return none, err
	}
	defer tx.Rollback()
	if err := lockPublication(ctx, tx, id); err != nil {
		return none, err
	}
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_publications WHERE workflow_id = ?`, id).Scan(&open); err != nil {
		if isNoTable(err) {
			return none, nil
		}
		return none, err
	}
	if open > 0 {
		return none, nil
	}
	before, err := statFile(h.files, id)
	if err != nil || before == nil {
		return none, nil
	}
	raw, existed, err := h.files.readRaw(id)
	if err != nil || !existed {
		return none, nil
	}
	if after, err := statFile(h.files, id); err != nil || after == nil || !after.same(before) {
		return none, nil
	}
	sha := shaHex(append([]byte{}, raw...))
	var known string
	switch err := tx.QueryRowContext(ctx, `SELECT file_sha FROM workflow_file_state WHERE workflow_id = ?`, id).Scan(&known); {
	case err == nil && known == sha:
		return none, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return none, err
	}
	wf, err := parseWorkflowFile(raw)
	if err != nil || wf == nil || wf.ID != id || wf.ProfileID == "" {
		return none, nil
	}
	if accept != nil && !accept(wf) {
		return none, nil
	}
	fileDef, err := definitionHash(wf)
	if err != nil {
		return none, nil
	}
	rows, err := getWorkflowFrom(ctx, tx, id)
	switch {
	case err == nil:
	case errors.Is(err, sql.ErrNoRows):
		rows = nil
	default:
		return none, err
	}
	res := ImportResult{Workflow: wf}
	if rows != nil {
		rowsDef, err := definitionHash(rows)
		if err != nil {
			return none, err
		}
		if rowsDef == fileDef {
			if err := h.recordFileState(ctx, tx, id, sha, fileDef); err != nil {
				return none, err
			}
			return none, tx.Commit()
		}
	}
	if err := h.sql.checkOwnIDs(ctx, wf); err != nil {
		return none, err
	}
	note := ImportNote
	if rows == nil {
		note = ""
	}
	if _, err := writeDefinitionRows(ctx, tx, wf, true, wf.Version, note); err != nil {
		return none, err
	}
	if err := h.recordFileState(ctx, tx, id, sha, fileDef); err != nil {
		return none, err
	}
	if err := tx.Commit(); err != nil {
		return none, fmt.Errorf("importing workflow %s: %w", id, err)
	}
	res.Imported, res.Created = true, rows == nil
	return res, nil
}

// recordFileState remembers, in tx, the bytes the rows were last reconciled with.
func (h *HybridWorkflowStore) recordFileState(ctx context.Context, tx *sql.Tx, id, sha, def string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_file_state (workflow_id, file_sha, definition_hash, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(workflow_id) DO UPDATE SET file_sha = excluded.file_sha,
			definition_hash = excluded.definition_hash, updated_at = excluded.updated_at`,
		id, sha, def, time.Now().UTC())
	return err
}

type importStamp struct {
	size    int64
	modTime time.Time
}

func (a *importStamp) same(b *importStamp) bool {
	return a.size == b.size && a.modTime.Equal(b.modTime)
}

func statFile(s *WorkflowFileStore, id string) (*importStamp, error) {
	path, err := s.filePath(id)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &importStamp{size: info.Size(), modTime: info.ModTime()}, nil
}
