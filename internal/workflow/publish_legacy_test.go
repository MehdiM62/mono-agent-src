package workflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

func dropFile(t *testing.T, h *HybridWorkflowStore, id string, active bool) {
	t.Helper()
	w := mirrorWF(id, id+"-trig", id+"-set")
	w.IsActive = active
	if err := h.files.SaveWorkflow(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

// A file-only workflow that older versions saved (it exists when the legacy set is recorded) keeps
// its active flag, before and after the claim.
func TestLegacyFileOnlyWorkflowStaysActive(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	dropFile(t, h, "wf-old", true)
	if err := h.ClaimLegacyFiles(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := h.ImportFile(ctx, "wf-old", nil)
	if err != nil || !res.Created {
		t.Fatalf("%+v %v", res, err)
	}
	if rows, _ := s.GetWorkflow(ctx, "wf-old"); !rows.IsActive {
		t.Fatal("a legacy file-only workflow lost its active flag")
	}
	// Nothing claimed yet: every file counts as legacy.
	h2, s2, _, _ := guardedRig(t)
	dropFile(t, h2, "wf-older", true)
	if _, err := h2.ImportFile(ctx, "wf-older", nil); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s2.GetWorkflow(ctx, "wf-older"); !rows.IsActive {
		t.Fatal("before any claim a file lost its active flag")
	}
}

// A file-only workflow that appears after the legacy set was recorded is imported inactive,
// whatever its file says, by the importer and by a plain read.
func TestFileThatAppearsAfterTheMarkerIsImportedInactive(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if err := h.ClaimLegacyFiles(ctx); err != nil {
		t.Fatal(err)
	}
	dropFile(t, h, "wf-dropped", true)
	res, err := h.ImportFile(ctx, "wf-dropped", nil)
	if err != nil || !res.Created {
		t.Fatalf("%+v %v", res, err)
	}
	if rows, _ := s.GetWorkflow(ctx, "wf-dropped"); rows.IsActive {
		t.Fatal("a dropped-in file started active")
	}
	var note string
	_ = s.db.QueryRow(`SELECT note FROM workflow_versions WHERE workflow_id = 'wf-dropped'`).Scan(&note)
	if note != ImportNewFileNote {
		t.Fatalf("note %q", note)
	}
	if got, _ := h.ImportedInactive(ctx); len(got) != 1 || got[0] != "wf-dropped" {
		t.Fatalf("doctor list: %v", got)
	}
	// A plain read of a second dropped file brings it in first and serves it inactive.
	dropFile(t, h, "wf-dropped2", true)
	got, err := h.GetWorkflow(ctx, "wf-dropped2")
	if err != nil || got == nil || got.IsActive {
		t.Fatalf("a read served the dropped file active: %+v %v", got, err)
	}
	if rows, _ := s.GetWorkflow(ctx, "wf-dropped2"); rows == nil || rows.IsActive {
		t.Fatal("the read did not bring the file in inactive")
	}
}

// Many processes claim at once: one list is written, once, and a later claim never widens it.
func TestLegacyClaimIsRecordedOnceByWhoeverGetsThereFirst(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	first, err := storage.NewDatabase(filepath.Join(root, "claim.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	files, err := NewWorkflowFileStore(filepath.Join(root, "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	seed := NewHybridWorkflowStore(files, NewSQLiteWorkflowStore(first.DB))
	dropFile(t, seed, "wf-a", true)
	dropFile(t, seed, "wf-b", false)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := storage.NewDatabase(filepath.Join(root, "claim.db"))
			if err != nil {
				errs <- err
				return
			}
			defer db.Close()
			errs <- NewHybridWorkflowStore(files, NewSQLiteWorkflowStore(db.DB)).ClaimLegacyFiles(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	dropFile(t, seed, "wf-c", true)
	if err := seed.ClaimLegacyFiles(ctx); err != nil { // a later claim must not widen the set
		t.Fatal(err)
	}
	var n int
	var raw string
	if err := first.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(value), '') FROM settings WHERE key = ?`, legacyFilesKey).Scan(&n, &raw); err != nil || n != 1 {
		t.Fatalf("%d markers (%v)", n, err)
	}
	var ids []string
	_ = json.Unmarshal([]byte(raw), &ids)
	if len(ids) != 2 {
		t.Fatalf("the legacy set is %v, want wf-a and wf-b only", ids)
	}
	if post, _ := postMarkerFile(ctx, first.DB, "wf-c"); !post {
		t.Fatal("a file dropped after the claim counts as legacy")
	}
	if post, _ := postMarkerFile(ctx, first.DB, "wf-a"); post {
		t.Fatal("a legacy file counts as new")
	}
}
