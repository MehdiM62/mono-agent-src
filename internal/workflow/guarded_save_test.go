package workflow

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
)

// guardedRig saves a two-node workflow with an approval and returns its review hash, the file's
// bytes and the edited nodes a guarded save would write.
func guardedRig(t *testing.T) (h *HybridWorkflowStore, s *SQLiteWorkflowStore, hash string, nodes []WorkflowNode) {
	t.Helper()
	ctx := context.Background()
	h, s = newHybridForTest(t)
	w := mirrorWF("wf-g", "g-trig", "g-set")
	if err := h.SaveWorkflow(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := h.ApproveDefinition(ctx, "wf-g"); err != nil {
		t.Fatal(err)
	}
	cur, err := h.GetWorkflow(ctx, "wf-g")
	if err != nil || cur == nil {
		t.Fatalf("%v", err)
	}
	if hash, err = ReviewHash(cur); err != nil {
		t.Fatal(err)
	}
	nodes = append([]WorkflowNode(nil), cur.Nodes...)
	nodes[1].Config = map[string]interface{}{"k": "edited"}
	t.Cleanup(func() { guardedFileFault, guardedCommitFault = nil, nil })
	return h, s, hash, nodes
}

func fileBytes(t *testing.T, h *HybridWorkflowStore, id string) []byte {
	t.Helper()
	b, existed, err := h.files.readRaw(id)
	if err != nil || !existed {
		t.Fatalf("reading the workflow file: %v existed=%v", err, existed)
	}
	return b
}

func sqlConfig(t *testing.T, s *SQLiteWorkflowStore, id, node string) any {
	t.Helper()
	w, err := s.GetWorkflow(context.Background(), id)
	if err != nil || w == nil {
		t.Fatalf("%v", err)
	}
	for _, n := range w.Nodes {
		if n.ID == node {
			return n.Config["k"]
		}
	}
	t.Fatalf("no node %s", node)
	return nil
}

func approvals(t *testing.T, s *SQLiteWorkflowStore, id string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM workflow_approvals WHERE workflow_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGuardedSavePublishesTheFileAndTheRowsTogether(t *testing.T) {
	ctx := context.Background()
	h, s, hash, nodes := guardedRig(t)
	if err := h.SaveNodesGuarded(ctx, "wf-g", nodes, SaveGuard{ExpectHash: hash, RevokeApprovals: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.GetWorkflow(ctx, "wf-g")
	if got.Nodes[1].Config["k"] != "edited" || sqlConfig(t, s, "wf-g", "g-set") != "edited" {
		t.Fatalf("file %v / sql %v", got.Nodes[1].Config, sqlConfig(t, s, "wf-g", "g-set"))
	}
	fh, _ := ReviewHash(got)
	sw, _ := s.GetWorkflow(ctx, "wf-g")
	sh, _ := ReviewHash(sw)
	if fh != sh || approvals(t, s, "wf-g") != 0 {
		t.Fatalf("the file and the rows disagree (%s / %s) or an approval survived", fh, sh)
	}
}

// A file that cannot be written leaves everything as it was and says so.
func TestGuardedSaveChangesNothingWhenTheFileCannotBeWritten(t *testing.T) {
	ctx := context.Background()
	h, s, hash, nodes := guardedRig(t)
	before := fileBytes(t, h, "wf-g")
	for name, inject := range map[string]func(){
		"an injected failure": func() { guardedFileFault = func() error { return errors.New("disk full") } },
		"a read-only folder": func() {
			if err := os.Chmod(h.files.dir, 0o500); err != nil {
				t.Skip("cannot make the folder read-only")
			}
			t.Cleanup(func() { _ = os.Chmod(h.files.dir, 0o700) })
			if f, err := os.CreateTemp(h.files.dir, "probe"); err == nil { // running as a user that ignores permissions
				f.Close()
				os.Remove(f.Name())
				t.Skip("the folder is writable despite chmod")
			}
		},
	} {
		guardedFileFault = nil
		inject()
		err := h.SaveNodesGuarded(ctx, "wf-g", nodes, SaveGuard{ExpectHash: hash, RevokeApprovals: true})
		if !errors.Is(err, ErrFileSyncFailed) {
			t.Fatalf("%s: err = %v, want ErrFileSyncFailed", name, err)
		}
		_ = os.Chmod(h.files.dir, 0o700)
		if string(fileBytes(t, h, "wf-g")) != string(before) {
			t.Errorf("%s: the file changed", name)
		}
		if sqlConfig(t, s, "wf-g", "g-set") != "v" || approvals(t, s, "wf-g") != 1 {
			t.Errorf("%s: the rows or the approval changed (config %v, approvals %d)", name, sqlConfig(t, s, "wf-g", "g-set"), approvals(t, s, "wf-g"))
		}
	}
}

// The transaction fails to commit after the file was published: the previous bytes come back.
func TestGuardedSaveRestoresTheFileWhenTheCommitFails(t *testing.T) {
	ctx := context.Background()
	h, s, hash, nodes := guardedRig(t)
	before := fileBytes(t, h, "wf-g")
	var published []byte
	guardedCommitFault = func() error {
		published = fileBytes(t, h, "wf-g") // the new file is on disk at this point
		return errors.New("commit failed")
	}
	err := h.SaveNodesGuarded(ctx, "wf-g", nodes, SaveGuard{ExpectHash: hash, RevokeApprovals: true})
	if err == nil {
		t.Fatal("a failed commit was reported as success")
	}
	if string(published) == string(before) {
		t.Fatal("the file was not yet published when the commit failed: the test proves nothing")
	}
	if string(fileBytes(t, h, "wf-g")) != string(before) {
		t.Error("the file was not restored")
	}
	if sqlConfig(t, s, "wf-g", "g-set") != "v" || approvals(t, s, "wf-g") != 1 {
		t.Error("the rows or the approval changed")
	}
}

// An operator's own save and a guarded apply never leave a mixed state: the apply is refused
// (the save won), or lands and the save then wins cleanly; either way the file and the rows agree.
func TestGuardedSaveAndAnOperatorSaveNeverMix(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 15; i++ {
		h, s, hash, nodes := guardedRig(t)
		saved := mirrorWF("wf-g", "g-trig", "g-set")
		saved.Name = "renamed by the operator"
		saved.Nodes[1].Config = map[string]interface{}{"k": "operator"}
		var wg sync.WaitGroup
		var applyErr, saveErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			applyErr = h.SaveNodesGuarded(ctx, "wf-g", nodes, SaveGuard{ExpectHash: hash, RevokeApprovals: true})
		}()
		go func() { defer wg.Done(); saveErr = h.SaveWorkflow(ctx, saved) }()
		wg.Wait()
		if saveErr != nil {
			t.Fatalf("the operator's save failed: %v", saveErr)
		}
		if applyErr != nil && !errors.Is(applyErr, ErrChangedSincePreview) {
			t.Fatalf("the apply failed with %v, want success or ErrChangedSincePreview", applyErr)
		}
		fw, _ := h.GetWorkflow(ctx, "wf-g")
		sw, _ := s.GetWorkflow(ctx, "wf-g")
		fh, _ := ReviewHash(fw)
		sh, _ := ReviewHash(sw)
		if fh != sh {
			t.Fatalf("run %d: the file and the rows disagree after an apply (%v) racing a save", i, applyErr)
		}
	}
}
