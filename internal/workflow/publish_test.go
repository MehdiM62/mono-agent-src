package workflow

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"
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

// An abandoned intent whose rows already hold its target is rolled forward: the file is made to
// say what the rows say.
func TestRecoveryRollsForwardWhenTheRowsHoldTheTarget(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	cur, _ := h.GetWorkflow(ctx, "wf-g")
	def, _ := definitionHash(cur)
	prev := fileBytes(t, h, "wf-g")
	if err := os.WriteFile(h.files.dir+"/wf-g.json", []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO workflow_publications
		(workflow_id, kind, state, target_hash, target_sha, prev_sha, prev_bytes, prev_existed, owner_pid, started_at)
		VALUES ('wf-g', 'save', 'pending', ?, 'x', 'y', ?, 1, 0, ?)`, def, prev, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if n, err := h.RecoverPublications(ctx); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := h.files.GetWorkflow(ctx, "wf-g")
	if got == nil || len(got.Nodes) != 2 || openIntentCount(t, s) != 0 {
		t.Fatalf("the file was not rewritten from the rows: %+v", got)
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
