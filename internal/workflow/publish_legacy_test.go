package workflow

import (
	"context"
	"encoding/json"
	"os"
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
	h, s := newHybridForTest(t) // nothing has used the store yet: the legacy set is not recorded
	dropFile(t, h, "wf-old", true)
	res, err := h.ImportFile(ctx, "wf-old", nil) // the first use records the set, and wf-old is in it
	if err != nil || !res.Created {
		t.Fatalf("%+v %v", res, err)
	}
	if rows, _ := s.GetWorkflow(ctx, "wf-old"); !rows.IsActive {
		t.Fatal("a legacy file-only workflow lost its active flag")
	}
	if post, _ := postMarkerFile(ctx, s.db, "wf-old"); post {
		t.Fatal("wf-old is not in the legacy set")
	}
}

// A file-only workflow that appears after the legacy set was recorded is neither imported nor served
// by anything that merely reads: it can name any profile, so it waits for an operator to adopt it,
// and is then created inactive.
func TestFileThatAppearsAfterTheMarkerWaitsToBeAdopted(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if err := h.ClaimLegacyFiles(ctx); err != nil {
		t.Fatal(err)
	}
	dropFile(t, h, "wf-dropped", true)
	if res, err := h.ImportFile(ctx, "wf-dropped", nil); err != nil || res.Imported {
		t.Fatalf("the importer took in a dropped file: %+v %v", res, err)
	}
	if got, err := h.GetWorkflow(ctx, "wf-dropped"); err != nil || got != nil {
		t.Fatalf("a read served a dropped file: %+v %v", got, err)
	}
	list, _ := h.ListAllProfilesWorkflows(ctx)
	for _, w := range list {
		if w.ID == "wf-dropped" {
			t.Fatal("a listing shows a dropped file")
		}
	}
	if got, _ := h.AdoptableFiles(ctx); len(got) != 1 || got[0] != "wf-dropped" {
		t.Fatalf("adoptable: %v", got)
	}
	if err := h.AdoptFile(ctx, "wf-dropped"); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.GetWorkflow(ctx, "wf-dropped")
	if rows == nil || rows.IsActive {
		t.Fatalf("adopted workflow: %+v", rows)
	}
	var note string
	_ = s.db.QueryRow(`SELECT note FROM workflow_versions WHERE workflow_id = 'wf-dropped'`).Scan(&note)
	if note != ImportNewFileNote {
		t.Fatalf("note %q", note)
	}
	if got, _ := h.ImportedInactive(ctx); len(got) != 1 || got[0] != "wf-dropped" {
		t.Fatalf("doctor list: %v", got)
	}
	if got, _ := h.GetWorkflow(ctx, "wf-dropped"); got == nil || got.IsActive {
		t.Fatalf("after adopting: %+v", got)
	}
	// A profile that does not exist cannot be adopted into.
	ghost := mirrorWF("wf-ghost-file", "gh-trig", "gh-set")
	ghost.ProfileID = "no-such-profile"
	if err := h.files.SaveWorkflow(ctx, ghost); err != nil {
		t.Fatal(err)
	}
	if err := h.AdoptFile(ctx, "wf-ghost-file"); err == nil {
		t.Fatal("adopted into a profile that does not exist")
	}
}

// A grandfathered id is released when its workflow is deleted: a file recreated under it later is a
// new file, not an older version's.
func TestDeletingAWorkflowReleasesItsLegacyId(t *testing.T) {
	ctx := context.Background()
	h, s := newHybridForTest(t)
	dropFile(t, h, "wf-gone", true)
	if _, err := h.ImportFile(ctx, "wf-gone", nil); err != nil {
		t.Fatal(err)
	}
	if post, _ := postMarkerFile(ctx, s.db, "wf-gone"); post {
		t.Fatal("setup: wf-gone should be legacy")
	}
	if err := h.DeleteWorkflow(ctx, "wf-gone"); err != nil {
		t.Fatal(err)
	}
	if post, _ := postMarkerFile(ctx, s.db, "wf-gone"); !post {
		t.Fatal("a deleted workflow's id is still grandfathered")
	}
	dropFile(t, h, "wf-gone", true)
	if got, _ := h.GetWorkflow(ctx, "wf-gone"); got != nil {
		t.Fatalf("a recreated file was served: %+v", got)
	}
}

// An unreadable marker fails closed.
func TestUnreadableLegacyMarkerFailsClosed(t *testing.T) {
	ctx := context.Background()
	_, s, _, _ := guardedRig(t)
	if _, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, 'not json') ON CONFLICT(key) DO UPDATE SET value = excluded.value`, legacyFilesKey); err != nil {
		t.Fatal(err)
	}
	if post, err := postMarkerFile(ctx, s.db, "wf-g"); err != nil || !post {
		t.Fatalf("post=%v err=%v", post, err)
	}
}

// Any process that uses the store claims the legacy set, not only the CLI's sync: reading is enough.
func TestFirstUseOfTheStoreRecordsTheLegacySet(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if _, err := h.GetWorkflow(ctx, "wf-g"); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, legacyFilesKey).Scan(&n)
	if n != 1 {
		t.Fatalf("%d markers after a read", n)
	}
	dropFile(t, h, "wf-late", true)
	if got, _ := h.GetWorkflow(ctx, "wf-late"); got != nil {
		t.Fatal("a file that appeared after the first use was served")
	}
}

// Listings say what the rows say: a file cannot move a workflow to another profile or switch it on,
// and a file whose content names another id than its name is ignored.
func TestListingsTakeOwnerAndActiveFlagFromTheRows(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if _, err := s.db.Exec(`INSERT INTO profiles (id, name) VALUES ('victim', 'Victim')`); err != nil {
		t.Fatal(err)
	}
	handEdit(t, h, "wf-g", func(w *Workflow) { w.IsActive = true; w.ProfileID = "victim" })
	data, _ := os.ReadFile(filepath.Join(h.files.dir, "wf-g.json"))
	if err := os.WriteFile(filepath.Join(h.files.dir, "other-name.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	all, err := h.ListAllProfilesWorkflows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, w := range all {
		if w.ID != "wf-g" {
			continue
		}
		seen++
		if w.IsActive || w.ProfileID != "default" {
			t.Fatalf("the listing took the file's word: active=%v profile=%q", w.IsActive, w.ProfileID)
		}
	}
	if seen != 1 {
		t.Fatalf("wf-g listed %d times", seen)
	}
	if mine, _ := h.ListWorkflows(ctx, "victim"); len(mine) != 0 {
		t.Fatalf("the victim profile lists %d workflows", len(mine))
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
