package workflow

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/storage"
)

func openIntentCount(t *testing.T, s *SQLiteWorkflowStore) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM workflow_publications`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A second change to a workflow whose first change is still open waits and then refuses.
func TestPublishRefusesWhileAnotherChangeIsOpen(t *testing.T) {
	ctx := context.Background()
	h, s, hash, nodes := guardedRig(t)
	if _, err := s.db.Exec(`INSERT INTO workflow_publications
		(workflow_id, kind, state, owner_pid, started_at) VALUES ('wf-g', 'save', 'pending', ?, ?)`,
		os.Getpid(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	old := publicationWait
	publicationWait = 150 * time.Millisecond
	t.Cleanup(func() { publicationWait = old })
	err := h.SaveNodesGuarded(ctx, "wf-g", nodes, SaveGuard{ExpectHash: hash})
	if !errors.Is(err, ErrPublicationBusy) {
		t.Fatalf("%v", err)
	}
	if got := sqlConfig(t, s, "wf-g", "g-set"); got == "edited" {
		t.Fatal("the refused change was written")
	}
}

// An abandoned intent is always rolled back, even when the rows' definition equals its target: an
// intent that survives proves the commit did not happen.
func TestRecoveryRollsBackEvenWhenTheRowsHoldTheTarget(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	cur, _ := h.GetWorkflow(ctx, "wf-g")
	def, _ := definitionHash(cur)
	prev := fileBytes(t, h, "wf-g")
	proposed := []byte("{}")
	if err := os.WriteFile(h.files.dir+"/wf-g.json", proposed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO workflow_publications
		(workflow_id, kind, state, target_hash, target_sha, prev_sha, prev_bytes, prev_existed, owner_pid, started_at)
		VALUES ('wf-g', 'save', 'pending', ?, ?, ?, ?, 1, 0, ?)`, def, shaHex(proposed), shaHex(prev), prev, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if n, err := h.RecoverPublications(ctx); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if !bytes.Equal(fileBytes(t, h, "wf-g"), prev) || openIntentCount(t, s) != 0 {
		t.Fatal("the proposed file was kept")
	}
}

// An intent whose owner is alive and recent is left alone by recovery.
func TestRecoveryLeavesALiveOwnersChangeAlone(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if _, err := s.db.Exec(`INSERT INTO workflow_publications
		(workflow_id, kind, state, owner_pid, started_at) VALUES ('wf-g', 'save', 'pending', ?, ?)`,
		os.Getpid(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	before := fileBytes(t, h, "wf-g")
	_, _ = h.RecoverPublications(ctx)
	if openIntentCount(t, s) != 1 || !bytes.Equal(before, fileBytes(t, h, "wf-g")) {
		t.Fatal("recovery touched a change that is still being made")
	}
}

// A failed commit puts the file back and leaves no intent.
func TestPublishLeavesNoIntentAfterAFailure(t *testing.T) {
	ctx := context.Background()
	h, s, hash, nodes := guardedRig(t)
	guardedCommitFault = func() error { return errors.New("commit failed") }
	if err := h.SaveNodesGuarded(ctx, "wf-g", nodes, SaveGuard{ExpectHash: hash}); err == nil {
		t.Fatal("expected a failure")
	}
	if openIntentCount(t, s) != 0 {
		t.Fatal("an intent was left behind")
	}
}

// A save that revokes approvals does it in the commit that writes the rows: a hook that refuses
// the commit keeps the approvals, and a commit that goes through removes them.
func TestSaveWorkflowRevokingRemovesApprovalsInTheSameCommit(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	cur, _ := h.GetWorkflow(ctx, "wf-g")
	refuse := WithTxHook(ctx, func(context.Context, *sql.Tx) error { return errors.New("no") })
	if err := h.SaveWorkflowRevoking(refuse, cur, ""); err == nil {
		t.Fatal("the hook should have refused")
	}
	if approvals(t, s, "wf-g") != 1 {
		t.Fatal("a refused save revoked approvals")
	}
	if err := h.SaveWorkflowRevoking(ctx, cur, ""); err != nil {
		t.Fatal(err)
	}
	if approvals(t, s, "wf-g") != 0 {
		t.Fatal("approvals survived")
	}
}

// A node edit whose file cannot be written is an error, and the rows keep the old nodes: the
// file step used to be dropped silently after the rows had changed.
func TestNodeEditWithAFailingFileIsAnErrorAndChangesNothing(t *testing.T) {
	ctx := context.Background()
	h, s, _, nodes := guardedRig(t)
	guardedFileFault = func() error { return errors.New("disk full") }
	err := h.SaveWorkflowNodes(ctx, "wf-g", nodes)
	if !errors.Is(err, ErrFileSyncFailed) {
		t.Fatalf("%v", err)
	}
	if sqlConfig(t, s, "wf-g", "g-set") == "edited" {
		t.Fatal("the rows changed although the file did not")
	}
	err = h.SaveWorkflowConnections(ctx, "wf-g", nil)
	if !errors.Is(err, ErrFileSyncFailed) {
		t.Fatalf("%v", err)
	}
	if got, _ := s.GetWorkflow(ctx, "wf-g"); len(got.Connections) != 1 {
		t.Fatal("the connections changed although the file did not")
	}
}

// A node edit lands in the file and the rows, and records its version in the same commit.
func TestNodeEditPublishesFileRowsAndVersionTogether(t *testing.T) {
	ctx := context.Background()
	h, s, _, nodes := guardedRig(t)
	before, _ := h.GetWorkflow(ctx, "wf-g")
	if err := h.SaveWorkflowNodes(ctx, "wf-g", nodes); err != nil {
		t.Fatal(err)
	}
	got, _ := h.GetWorkflow(ctx, "wf-g")
	if got.Nodes[1].Config["k"] != "edited" || sqlConfig(t, s, "wf-g", "g-set") != "edited" {
		t.Fatal("file and rows disagree")
	}
	if got.Version != before.Version+1 {
		t.Fatalf("version %d -> %d", before.Version, got.Version)
	}
	if v, _ := h.GetWorkflowVersion(ctx, "wf-g", got.Version); v == nil {
		t.Fatal("no version recorded")
	}
}

// An operator edit records its approval in the commit that writes the rows; a refused commit
// records nothing.
func TestWithApprovalRecordsTheNewDefinitionInTheCommit(t *testing.T) {
	ctx := context.Background()
	h, s, _, nodes := guardedRig(t)
	refuse := WithApproval(WithTxHook(ctx, func(context.Context, *sql.Tx) error { return errors.New("no") }))
	if err := h.SaveWorkflowNodes(refuse, "wf-g", nodes); err == nil {
		t.Fatal("the hook should have refused")
	}
	if approvals(t, s, "wf-g") != 1 {
		t.Fatalf("a refused edit changed approvals: %d", approvals(t, s, "wf-g"))
	}
	if err := h.SaveWorkflowNodes(WithApproval(ctx), "wf-g", nodes); err != nil {
		t.Fatal(err)
	}
	cur, _ := h.GetWorkflow(ctx, "wf-g")
	hash, _ := definitionHash(cur)
	if ok, _ := h.DefinitionApproved(ctx, "wf-g", hash); !ok {
		t.Fatal("the edited definition was not approved")
	}
	// Without it the edit is left for a person to approve.
	nodes[1].Config = map[string]interface{}{"k": "again"}
	if err := h.SaveWorkflowNodes(ctx, "wf-g", nodes); err != nil {
		t.Fatal(err)
	}
	cur, _ = h.GetWorkflow(ctx, "wf-g")
	hash, _ = definitionHash(cur)
	if ok, _ := h.DefinitionApproved(ctx, "wf-g", hash); ok {
		t.Fatal("an edit without WithApproval was approved")
	}
}

// Removing a node takes its connections out of the file as well as the rows.
func TestRemovingANodeTakesItsConnectionsFromTheFileToo(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	cur, _ := h.GetWorkflow(ctx, "wf-g")
	if err := h.SaveWorkflowNodes(ctx, "wf-g", cur.Nodes[:1]); err != nil {
		t.Fatal(err)
	}
	f, _ := h.files.GetWorkflow(ctx, "wf-g")
	r, _ := s.GetWorkflow(ctx, "wf-g")
	if len(f.Connections) != 0 || len(r.Connections) != 0 {
		t.Fatalf("file %d rows %d connections left", len(f.Connections), len(r.Connections))
	}
}

// The transactions of a publication flush to disk before they return; the connection goes back to
// the pool as it was.
func TestPublicationTransactionsSyncToDisk(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	var before int
	_ = s.db.QueryRow(`PRAGMA synchronous`).Scan(&before)
	tx, done, err := h.beginDurable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var inside int
	if err := tx.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&inside); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	done()
	var after int
	_ = s.db.QueryRow(`PRAGMA synchronous`).Scan(&after)
	if inside != 2 || after != before {
		t.Fatalf("synchronous: inside %d (want 2 = FULL), before %d, after %d", inside, before, after)
	}
}

// BenchmarkPublish measures one guarded save (intent, file, commit) on a scratch database.
func BenchmarkPublish(b *testing.B) {
	ctx := context.Background()
	files, err := NewWorkflowFileStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	db, err := storage.NewDatabase(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		b.Fatal(err)
	}
	sqlStore := NewSQLiteWorkflowStore(db.DB)
	h := NewHybridWorkflowStore(files, sqlStore)
	if err := h.SaveWorkflow(ctx, mirrorWF("wf-b", "b-trig", "b-set")); err != nil {
		b.Fatal(err)
	}
	cur, _ := h.GetWorkflow(ctx, "wf-b")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodes := append([]WorkflowNode(nil), cur.Nodes...)
		nodes[1].Config = map[string]interface{}{"k": i}
		if err := h.SaveWorkflowNodes(ctx, "wf-b", nodes); err != nil {
			b.Fatal(err)
		}
	}
}

// A change that starts while files are being read makes the read run again, so a file replaced
// before the read opened it is never trusted.
func TestStableFileReadRepeatsWhenAChangeStartsDuringIt(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	calls := 0
	var last map[string]bool
	err := h.stableFileRead(ctx, func(open map[string]bool) error {
		calls++
		last = open
		if calls == 1 {
			_, err := s.db.Exec(`INSERT INTO workflow_publications (workflow_id, kind, state, owner_pid, started_at)
				VALUES ('wf-g', 'save', 'pending', ?, ?)`, os.Getpid(), time.Now().UTC())
			return err
		}
		return nil
	})
	if err != nil || calls != 2 || !last["wf-g"] {
		t.Fatalf("calls %d open %v err %v", calls, last, err)
	}
}

// A guarded save with no expected hash never matches: it is refused for an existing workflow, and
// a missing workflow is reported instead of crashing.
func TestGuardedSaveWithAnEmptyHashIsRefused(t *testing.T) {
	ctx := context.Background()
	h, s, _, nodes := guardedRig(t)
	if err := h.SaveNodesGuarded(ctx, "wf-g", nodes, SaveGuard{}); !errors.Is(err, ErrChangedSincePreview) {
		t.Fatalf("existing workflow: %v", err)
	}
	if sqlConfig(t, s, "wf-g", "g-set") == "edited" {
		t.Fatal("the refused edit was written")
	}
	if err := h.SaveNodesGuarded(ctx, "no-such", nodes, SaveGuard{}); err == nil {
		t.Fatal("a missing workflow must be an error")
	}
}

// Connections saved with empty handles are normalized before the version is worked out: saving the
// same workflow again is not a new version, and an operator save approves what was published.
func TestSaveNormalizesConnectionHandlesBeforeHashingAndVersioning(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	w := mirrorWF("wf-h", "h-trig", "h-set")
	w.Connections[0].SourceHandle, w.Connections[0].TargetHandle = "", ""
	if err := h.SaveWorkflow(WithApproval(ctx), w); err != nil {
		t.Fatal(err)
	}
	got, _ := h.GetWorkflow(ctx, "wf-h")
	hash, _ := definitionHash(got)
	if ok, _ := h.DefinitionApproved(ctx, "wf-h", hash); !ok {
		t.Fatal("the published definition is not the one that was approved")
	}
	again := mirrorWF("wf-h", "h-trig", "h-set")
	again.Connections[0].SourceHandle, again.Connections[0].TargetHandle = "", ""
	if err := h.SaveWorkflow(ctx, again); err != nil {
		t.Fatalf("saving the same workflow again: %v", err)
	}
	if versionCount(t, s, "wf-h") != 1 {
		t.Fatalf("%d versions for one definition", versionCount(t, s, "wf-h"))
	}
}
