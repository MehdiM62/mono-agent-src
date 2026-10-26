package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"
)

// Publication kinds.
const (
	PubSave   = "save"
	PubDelete = "delete"
	PubImport = "import"
)

// ErrPublicationBusy: another publication of this workflow is open.
var ErrPublicationBusy = errors.New("another change to this workflow is in progress")

// ErrPublicationLost: the publication was rolled back by another process before it committed
// (its file was put back), so nothing was changed.
var ErrPublicationLost = errors.New("the change was rolled back by another process before it committed")

const publicationLease = 30 * time.Second

// publicationWait is how long Publish retries while another change of the same workflow is open.
var publicationWait = 5 * time.Second

// Publication describes one change of a workflow's definition, made through Publish.
type Publication struct {
	WorkflowID string
	Kind       string // PubSave (default), PubDelete or PubImport
	Origin     string // the writer, kept on the intent row
	Note       string // the note of the version it records
	// ExpectHash is the ReviewHash the committed workflow must still have (ErrChangedSincePreview).
	ExpectHash string
	// Guarded compares ExpectHash even when it is empty (an empty hash then never matches an
	// existing workflow, and a missing workflow is refused).
	Guarded bool
	// RevokeApprovals removes every stored approval in the commit.
	RevokeApprovals bool
	// StoredVersion is the version the caller saw before the change.
	StoredVersion int
	// RequireGraph refuses (ErrSharedIDs) a definition whose node or connection ids belong to
	// another workflow, instead of saving it without its graph rows.
	RequireGraph bool
	// KeepFileless leaves a workflow that has no file without one (rows only).
	KeepFileless bool
	// Build maps the committed definition (nil when there are no rows) to the one to publish.
	Build func(cur *Workflow) (*Workflow, error)
}

type pubPlan struct {
	cur        *Workflow
	target     *Workflow
	graph      bool
	baseHash   string
	version    int
	data       []byte
	targetSHA  string
	targetHash string
}

func shaHex(b []byte) string {
	if b == nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// lockPublication takes the database write lock for the rest of tx (a write statement acquires
// it) so the reads that follow cannot be overtaken.
func lockPublication(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `UPDATE workflow_publications SET state = state WHERE workflow_id = ?`, id)
	return err
}

// Publish is the one path by which a definition reaches both stores. The rows are the commit
// point and the file a projection of them; because they cannot share a transaction, an intent is
// recorded first (its own transaction, visible to every process), then the file is published,
// then the rows commit together with the version and the intent's removal. A process that dies
// in between leaves the intent behind, and whoever sees it next puts the file back (see
// resolveIntent). It returns the published workflow, nil for a delete.
func (h *HybridWorkflowStore) Publish(ctx context.Context, p Publication) (*Workflow, error) {
	if p.Kind == "" {
		p.Kind = PubSave
	}
	deadline := time.Now().Add(publicationWait)
	for {
		w, err := h.publishOnce(ctx, p)
		if !errors.Is(err, ErrPublicationBusy) || time.Now().After(deadline) {
			if p.Kind == PubDelete {
				return w, err
			}
			return w, h.saved(ctx, p.WorkflowID, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// beginDurable starts a transaction on a connection that flushes its commit to disk
// (synchronous=FULL, the pool's default is NORMAL). The intent must reach the disk before the file
// is replaced: otherwise a power cut could keep the new file and lose the intent, and the file
// would look like an outside edit. done puts the connection back as it was and must run after the
// transaction has ended.
func (h *HybridWorkflowStore) beginDurable(ctx context.Context) (tx *sql.Tx, done func(), err error) {
	conn, err := h.sql.db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	var level int
	if err := conn.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&level); err != nil {
		conn.Close()
		return nil, nil, err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA synchronous = FULL`); err != nil {
		conn.Close()
		return nil, nil, err
	}
	done = func() {
		_, _ = conn.ExecContext(context.Background(), fmt.Sprintf(`PRAGMA synchronous = %d`, level))
		conn.Close()
	}
	if tx, err = conn.BeginTx(ctx, nil); err != nil {
		done()
		return nil, nil, err
	}
	return tx, done, nil
}

func (h *HybridWorkflowStore) publishOnce(ctx context.Context, p Publication) (*Workflow, error) {
	if h.files != nil {
		if err := h.recoverID(ctx, p.WorkflowID); err != nil {
			return nil, fmt.Errorf("%w: workflow %s: %v", ErrFileInconsistent, p.WorkflowID, err)
		}
	}
	tx, done, err := h.beginDurable(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning publication: %w", err)
	}
	defer done()
	defer tx.Rollback()
	if err := lockPublication(ctx, tx, p.WorkflowID); err != nil {
		return nil, fmt.Errorf("locking workflow %s: %w", p.WorkflowID, err)
	}
	var busy int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_publications WHERE workflow_id = ?`, p.WorkflowID).Scan(&busy); err != nil {
		return nil, err
	}
	if busy > 0 {
		return nil, ErrPublicationBusy
	}
	plan, err := h.plan(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	var prev []byte
	var existed bool
	if h.files != nil {
		if prev, existed, err = h.files.readRaw(p.WorkflowID); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFileSyncFailed, err)
		}
	}
	if h.files == nil || (p.KeepFileless && !existed && p.Kind != PubDelete) {
		return h.publishRowsOnly(ctx, tx, p, plan)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_publications
			(workflow_id, kind, state, base_hash, target_hash, target_sha, prev_sha, prev_bytes, prev_existed,
			 next_version, origin, owner_pid, started_at, note)
		VALUES (?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.WorkflowID, p.Kind, plan.baseHash, plan.targetHash, plan.targetSHA, shaHex(prev), prev, boolToInt(existed),
		plan.version, p.Origin, os.Getpid(), time.Now().UTC(), p.Note)
	if err != nil {
		return nil, fmt.Errorf("recording the intent for workflow %s: %w", p.WorkflowID, err)
	}
	intentID, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("recording the intent for workflow %s: %w", p.WorkflowID, err)
	}
	_ = failpoint("after-intent")

	if err := h.publishFile(ctx, p, plan, intentID); err != nil {
		if errors.Is(err, ErrPublicationLost) {
			return nil, err // nothing was written: the file step never started
		}
		return nil, h.abort(ctx, intentID, p.WorkflowID, err)
	}
	var window error
	if guardedCommitFault != nil {
		window = guardedCommitFault()
	}
	if window == nil {
		window = failpoint("after-rename")
	}
	if window != nil {
		return nil, h.abort(ctx, intentID, p.WorkflowID, fmt.Errorf("committing guarded save: %w", window))
	}
	w, err := h.commitPublication(ctx, p, plan, intentID)
	if err != nil {
		if errors.Is(err, ErrPublicationLost) {
			h.repairAfterLost(ctx, p.WorkflowID, plan.targetSHA)
			return nil, err
		}
		return nil, h.abort(ctx, intentID, p.WorkflowID, err)
	}
	_ = failpoint("after-commit")
	return w, nil
}

// plan reads the committed workflow inside tx and works out what the publication writes.
func (h *HybridWorkflowStore) plan(ctx context.Context, tx *sql.Tx, p Publication) (*pubPlan, error) {
	pl := &pubPlan{}
	cur, err := getWorkflowFrom(ctx, tx, p.WorkflowID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		pl.cur = cur
		if pl.baseHash, err = ReviewHash(cur); err != nil {
			return nil, err
		}
	}
	if p.ExpectHash != "" || p.Guarded {
		if pl.cur == nil {
			return nil, fmt.Errorf("workflow %s not found", p.WorkflowID)
		}
		if pl.baseHash != p.ExpectHash {
			return nil, ErrChangedSincePreview
		}
	}
	if p.Kind == PubDelete {
		return pl, nil
	}
	if pl.target, err = p.Build(pl.cur); err != nil {
		return nil, err
	}
	t := pl.target
	t.ID = p.WorkflowID
	for i := range t.Connections {
		if t.Connections[i].ID == "" {
			t.Connections[i].ID = newID()
		}
	}
	pl.graph = true
	if err := h.sql.checkOwnIDs(ctx, t); err != nil {
		if !errors.Is(err, ErrSharedIDs) {
			return nil, err
		}
		if p.RequireGraph {
			return nil, err
		}
		pl.graph = false
	}
	if pl.targetHash, err = definitionHash(t); err != nil {
		return nil, err
	}
	if pl.version, err = nextVersionTx(ctx, tx, t, p.StoredVersion, pl.cur); err != nil {
		return nil, err
	}
	t.Version = pl.version
	if h.files != nil {
		if pl.data, err = h.files.renderWorkflow(t); err != nil {
			return nil, err
		}
		pl.targetSHA = shaHex(pl.data)
	}
	return pl, nil
}

// nextVersionTx is the version recordVersionTx will give w: the latest one when the definition is
// unchanged, the next one otherwise.
func nextVersionTx(ctx context.Context, tx *sql.Tx, w *Workflow, stored int, cur *Workflow) (int, error) {
	hash, err := snapshotOf(w).hash()
	if err != nil {
		return 0, err
	}
	var latest int
	var latestHash string
	err = tx.QueryRowContext(ctx, `
		SELECT version, definition_hash FROM workflow_versions
		WHERE workflow_id = ? ORDER BY version DESC LIMIT 1`, w.ID).Scan(&latest, &latestHash)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("reading latest version of workflow %s: %w", w.ID, err)
	}
	if latest != 0 && latestHash == hash {
		return latest, nil
	}
	rowVersion := 0
	if cur != nil {
		rowVersion = cur.Version
	}
	return max(latest, max(stored, rowVersion)) + 1, nil
}

// applyRows is the commit body shared by the file and the rows-only paths: the rows, the version,
// the approvals and the caller's transaction hook.
func (h *HybridWorkflowStore) applyRows(ctx context.Context, tx *sql.Tx, p Publication, pl *pubPlan) error {
	if p.Kind == PubDelete {
		if _, err := tx.ExecContext(ctx, deleteWorkflowRowSQL, p.WorkflowID); err != nil {
			return fmt.Errorf("removing workflow %s: %w", p.WorkflowID, err)
		}
		return nil
	}
	if pl.graph {
		// Planning looked at who owns the node and connection ids, but another change may have
		// claimed one since; an upsert would move it out from under that workflow.
		if err := checkOwnIDsOn(ctx, tx, pl.target); err != nil {
			return err
		}
	}
	v, err := writeDefinitionRows(ctx, tx, pl.target, pl.graph, p.StoredVersion, p.Note)
	if err != nil {
		return err
	}
	if v != pl.version {
		return fmt.Errorf("workflow %s: the version moved from %d to %d during the change", p.WorkflowID, pl.version, v)
	}
	if approvalFrom(ctx) && p.Kind != PubDelete {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO workflow_approvals (workflow_id, definition_hash) VALUES (?, ?)`,
			p.WorkflowID, pl.targetHash); err != nil && !isNoTable(err) {
			return fmt.Errorf("recording the approval: %w", err)
		}
	}
	if p.RevokeApprovals {
		if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_approvals WHERE workflow_id = ?`, p.WorkflowID); err != nil && !isNoTable(err) {
			return fmt.Errorf("revoking approvals: %w", err)
		}
	}
	if hook := txHookFrom(ctx); hook != nil {
		if err := hook(ctx, tx); err != nil {
			return &TxHookError{Err: err}
		}
	}
	return nil
}

// deleteWorkflowRowSQL removes a workflow's row; its nodes, connections, approvals and versions
// follow by cascade.
const deleteWorkflowRowSQL = `DELETE FROM workflows WHERE id = ?`

// publishRowsOnly commits a publication that has no file to publish, in the transaction that
// planned it.
func (h *HybridWorkflowStore) publishRowsOnly(ctx context.Context, tx *sql.Tx, p Publication, pl *pubPlan) (*Workflow, error) {
	if err := h.applyRows(ctx, tx, p, pl); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing guarded save: %w", err)
	}
	return pl.target, nil
}

// publishFile is step 2: the file reaches its target bytes (or is removed, for a delete). The
// ownership check and the file write happen inside one transaction that holds the write lock, as
// recovery does, so recovery cannot remove this intent between the check and the rename: either it
// ran first (ErrPublicationLost, nothing written) or the rename completes first and recovery then
// finds the file and rolls it back.
func (h *HybridWorkflowStore) publishFile(ctx context.Context, p Publication, pl *pubPlan, intentID int64) error {
	if err := failpoint("before-fence"); err != nil {
		return err
	}
	// The slow part (writing and flushing the new bytes) happens before the lock is taken; under
	// the lock only the rename and the folder flush remain.
	staged := ""
	if p.Kind != PubDelete {
		var err error
		if staged, err = h.files.stageRaw(p.WorkflowID, pl.data); err != nil {
			return fmt.Errorf("%w: %v", ErrFileSyncFailed, err)
		}
		defer func() {
			if staged != "" {
				_ = os.Remove(staged)
			}
		}()
	}
	tx, done, err := h.beginDurable(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFileSyncFailed, err)
	}
	defer done()
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workflow_publications SET state = state WHERE id = ? AND state = 'pending'`, intentID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFileSyncFailed, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPublicationLost
	}
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	err = failpoint("before-rename")
	if err == nil && guardedFileFault != nil {
		err = guardedFileFault()
	}
	if err == nil {
		if p.Kind == PubDelete {
			err = h.files.removeFile(p.WorkflowID)
		} else {
			err = h.files.installRaw(p.WorkflowID, staged)
			staged = "" // installed, or removed by installRaw
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrFileSyncFailed, err)
	}
	return nil
}

// repairAfterLost runs in a writer whose intent was removed by someone else after it had written
// its file: when the file still holds this writer's bytes and no other change is open, it rewrites the
// file from the committed rows, so the file never outlives the rows it was meant to describe.
func (h *HybridWorkflowStore) repairAfterLost(ctx context.Context, id, targetSHA string) {
	tx, done, err := h.beginDurable(ctx)
	if err != nil {
		return
	}
	defer done()
	defer tx.Rollback()
	if err := lockPublication(ctx, tx, id); err != nil {
		return
	}
	var open int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_publications WHERE workflow_id = ?`, id).Scan(&open); err != nil || open > 0 {
		return
	}
	raw, there, err := h.files.readRaw(id)
	now := ""
	if there {
		now = shaHex(append([]byte{}, raw...))
	}
	if err != nil || now != targetSHA {
		return
	}
	// Rewrite the file from the committed rows (another save may have committed since this
	// writer's bytes were derived); with no rows the workflow has no file.
	rows, err := getWorkflowFrom(ctx, tx, id)
	switch {
	case err == nil:
		var data []byte
		if data, err = h.files.renderWorkflow(rows); err == nil {
			err = h.files.writeRaw(id, data)
		}
	case errors.Is(err, sql.ErrNoRows):
		err = h.files.removeFile(id)
	}
	if err == nil {
		_ = tx.Commit()
	}
}

// commitPublication is step 3: the rows, the version, the approvals and the removal of the
// intent commit together.
func (h *HybridWorkflowStore) commitPublication(ctx context.Context, p Publication, pl *pubPlan, intentID int64) (*Workflow, error) {
	if err := failpoint("before-tx2"); err != nil {
		return nil, err
	}
	tx, done, err := h.beginDurable(ctx)
	if err != nil {
		return nil, fmt.Errorf("committing guarded save: %w", err)
	}
	defer done()
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workflow_publications SET state = state WHERE id = ? AND state = 'pending'`, intentID)
	if err != nil {
		return nil, fmt.Errorf("committing guarded save: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrPublicationLost
	}
	base := ""
	cur, err := getWorkflowFrom(ctx, tx, p.WorkflowID)
	switch {
	case err == nil:
		if base, err = ReviewHash(cur); err != nil {
			return nil, err
		}
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	if base != pl.baseHash {
		return nil, ErrChangedSincePreview
	}
	if err := h.applyRows(ctx, tx, p, pl); err != nil {
		return nil, err
	}
	if p.Kind == PubDelete {
		if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_file_state WHERE workflow_id = ?`, p.WorkflowID); err != nil {
			return nil, err
		}
	} else if _, err := tx.ExecContext(ctx, `
		INSERT INTO workflow_file_state (workflow_id, file_sha, definition_hash, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(workflow_id) DO UPDATE SET file_sha = excluded.file_sha,
			definition_hash = excluded.definition_hash, updated_at = excluded.updated_at`,
		p.WorkflowID, pl.targetSHA, pl.targetHash, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("recording the file state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_publications WHERE id = ?`, intentID); err != nil {
		return nil, err
	}
	if err := failpoint("inside-tx2-before-commit"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing guarded save: %w", err)
	}
	return pl.target, nil
}

// abort undoes a publication that failed in this process: the file goes back to what the rows
// say, the intent is removed, and err is returned (joined with what could not be undone).
func (h *HybridWorkflowStore) abort(ctx context.Context, intentID int64, id string, err error) error {
	if rerr := h.rollbackIntent(ctx, intentID); rerr != nil {
		// Nobody owns the unfinished change now: the next process to look retries the restore.
		_, _ = h.sql.db.ExecContext(ctx, `UPDATE workflow_publications SET owner_pid = 0 WHERE id = ?`, intentID)
		return errors.Join(err, fmt.Errorf("%w: workflow %s: %v", ErrFileInconsistent, id, rerr))
	}
	return err
}

type approvalKey struct{}

// WithApproval makes the next publication made with ctx also record its resulting definition as
// approved, in the commit that writes the rows (the operator's own edits, which have no window
// between the save and the approval). RevokeApprovals runs after it and wins.
func WithApproval(ctx context.Context) context.Context {
	return context.WithValue(ctx, approvalKey{}, true)
}

func approvalFrom(ctx context.Context) bool {
	v, _ := ctx.Value(approvalKey{}).(bool)
	return v
}
