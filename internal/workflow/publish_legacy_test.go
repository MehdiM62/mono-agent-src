package workflow

import (
	"context"
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

func markerCount(t *testing.T, s *SQLiteWorkflowStore) int {
	t.Helper()
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, legacyDoneKey).Scan(&n)
	return n
}

// The workflow files older versions left become workflows when the store is first used, as they
// always did (the active flag is kept), and the claim is recorded once.
func TestFilesOlderVersionsLeftAreTakenInOnceAndKeepTheirFlag(t *testing.T) {
	ctx := context.Background()
	h, s := newHybridForTest(t) // nothing has used the store yet
	dropFile(t, h, "wf-old", true)
	if got, err := h.GetWorkflow(ctx, "wf-old"); err != nil || got == nil || !got.IsActive {
		t.Fatalf("a read of an older version's file: %+v %v", got, err)
	}
	if rows, _ := s.GetWorkflow(ctx, "wf-old"); rows == nil || !rows.IsActive {
		t.Fatalf("rows: %+v", rows)
	}
	if markerCount(t, s) != 1 {
		t.Fatal("the claim was not recorded")
	}
}

// A file with no rows that appears after the claim is not imported, not served and not listed; an
// operator adopts it by naming the file they looked at, and it starts inactive.
func TestFileThatAppearsAfterTheClaimWaitsToBeAdopted(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t) // used already: the claim is recorded
	dropFile(t, h, "wf-dropped", true)
	if res, err := h.ImportFile(ctx, "wf-dropped", nil); err != nil || res.Imported {
		t.Fatalf("the importer took in a dropped file: %+v %v", res, err)
	}
	if got, err := h.GetWorkflow(ctx, "wf-dropped"); err != nil || got != nil {
		t.Fatalf("a read served a dropped file: %+v %v", got, err)
	}
	for _, w := range mustList(t, h) {
		if w.ID == "wf-dropped" {
			t.Fatal("a listing shows a dropped file")
		}
	}
	shown, more, err := h.AdoptableFiles(ctx)
	if err != nil || len(shown) != 1 || more != 0 || shown[0].ID != "wf-dropped" || shown[0].ProfileID != "default" || len(shown[0].SHA12) != 12 {
		t.Fatalf("adoptable: %+v %d %v", shown, more, err)
	}
	a := shown[0]
	if len(a.Triggers) != 1 || a.Triggers[0] != "trigger.manual" {
		t.Fatalf("triggers: %v", a.Triggers)
	}
	if err := h.AdoptFile(ctx, "wf-dropped", a.ProfileID, "000000000000"); err == nil {
		t.Fatal("adopted a file other than the one that was looked at")
	}
	if err := h.AdoptFile(ctx, "wf-dropped", "some-other-profile", a.SHA12); err == nil {
		t.Fatal("adopted into a profile the file does not name")
	}
	if err := h.AdoptFile(ctx, "wf-dropped", a.ProfileID, a.SHA12); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.GetWorkflow(ctx, "wf-dropped")
	if rows == nil || rows.IsActive {
		t.Fatalf("adopted workflow: %+v", rows)
	}
	var note string
	_ = s.db.QueryRow(`SELECT note FROM workflow_versions WHERE workflow_id = 'wf-dropped'`).Scan(&note)
	if note != ImportNewFileNote || approvals(t, s, "wf-dropped") != 0 {
		t.Fatalf("note %q, approvals %d", note, approvals(t, s, "wf-dropped"))
	}
	if got, _ := h.ImportedInactive(ctx); len(got) != 1 || got[0] != "wf-dropped" {
		t.Fatalf("doctor list: %v", got)
	}
}

// Adopting never replaces a stored workflow and never starts one: a workflow that has rows is
// refused (it would be an outside-edit import with revoked approvals), and a file that says
// is_active:true is adopted inactive.
func TestAdoptRefusesAStoredWorkflowAndAlwaysStartsInactive(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	handEdit(t, h, "wf-g", func(w *Workflow) { w.Nodes[1].Config = map[string]interface{}{"k": "changed"} })
	sha := shaHex(fileBytes(t, h, "wf-g"))[:12]
	if err := h.AdoptFile(ctx, "wf-g", "default", sha); err == nil {
		t.Fatal("a workflow that has rows was adopted")
	}
	if sqlConfig(t, s, "wf-g", "g-set") == "changed" || approvals(t, s, "wf-g") != 1 {
		t.Fatal("adopting acted as an outside-edit import")
	}
	dropFile(t, h, "wf-active-file", true)
	shown, _, _ := h.AdoptableFiles(ctx)
	if len(shown) != 1 {
		t.Fatalf("%+v", shown)
	}
	if err := h.AdoptFile(ctx, "wf-active-file", shown[0].ProfileID, shown[0].SHA12); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.GetWorkflow(ctx, "wf-active-file"); rows.IsActive {
		t.Fatal("an adopted workflow started active")
	}
}

// An id that once had a workflow gets no special standing: after its rows are gone (deleted, or its
// profile deleted) a file recreated under it is just a file.
func TestARecreatedFileForAGoneWorkflowIsNotServed(t *testing.T) {
	ctx := context.Background()
	h, s := newHybridForTest(t)
	dropFile(t, h, "wf-gone", true)
	if got, _ := h.GetWorkflow(ctx, "wf-gone"); got == nil {
		t.Fatal("setup: the older version's file should be taken in")
	}
	if err := h.DeleteWorkflow(ctx, "wf-gone"); err != nil {
		t.Fatal(err)
	}
	dropFile(t, h, "wf-gone", true)
	if got, _ := h.GetWorkflow(ctx, "wf-gone"); got != nil {
		t.Fatalf("a recreated file was served: %+v", got)
	}
	// The same when the rows go some other way (a profile delete removes them directly).
	dropFile(t, h, "wf-gone2", true)
	if got, _ := h.GetWorkflow(ctx, "wf-gone2"); got != nil {
		t.Fatalf("a file that appeared after the claim was served: %+v", got)
	}
	if rows, _ := s.GetWorkflow(ctx, "wf-gone"); rows != nil {
		t.Fatal("rows came back")
	}
	for _, w := range mustList(t, h) {
		if w.ID == "wf-gone" || w.ID == "wf-gone2" {
			t.Fatalf("a listing shows %s", w.ID)
		}
	}
}

// Nothing is served from a file with no rows even when the claim never got recorded (a busy or
// broken database): the store fails closed.
func TestAFailedClaimServesNothingFromAFileWithNoRows(t *testing.T) {
	ctx := context.Background()
	h, _ := newHybridForTest(t)
	h.claimMu.Lock()
	h.claimed = true // pretend the claim ran: it did not, and there is no marker
	h.claimMu.Unlock()
	dropFile(t, h, "wf-x", true)
	if got, err := h.GetWorkflow(ctx, "wf-x"); err != nil || got != nil {
		t.Fatalf("served %+v (%v)", got, err)
	}
	for _, w := range mustList(t, h) {
		if w.ID == "wf-x" {
			t.Fatal("listed")
		}
	}
}

// An edit-import of a workflow with no rows returns before the write lock or the file is touched:
// the sync calls it for every file on every command.
func TestImportOfAFileWithNoRowsDoesNotTakeTheWriteLock(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	dropFile(t, h, "wf-nolock", true)
	release := holdWriteLock(t, s)
	defer release()
	quickly(t, "the import", func() {
		if res, err := h.ImportFile(ctx, "wf-nolock", nil); err != nil || res.Imported {
			t.Errorf("%+v %v", res, err)
		}
	})
}

// Many processes claim at once: the files are taken in once, the claim is recorded once, and a file
// that appears afterwards is not taken in by a later claim.
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
			_, err = NewHybridWorkflowStore(files, NewSQLiteWorkflowStore(db.DB)).ClaimLegacyFiles(ctx)
			errs <- err
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
	if _, err := seed.ClaimLegacyFiles(ctx); err != nil { // a later claim does nothing
		t.Fatal(err)
	}
	var n, active int
	_ = first.DB.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, legacyDoneKey).Scan(&n)
	if n != 1 {
		t.Fatalf("%d markers", n)
	}
	for id, want := range map[string]int{"wf-a": 1, "wf-b": 0} {
		if err := first.DB.QueryRow(`SELECT is_active FROM workflows WHERE id = ?`, id).Scan(&active); err != nil || active != want {
			t.Fatalf("%s: active %d (%v), want %d", id, active, err, want)
		}
	}
	if err := first.DB.QueryRow(`SELECT COUNT(*) FROM workflows WHERE id = 'wf-c'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a file that appeared after the claim got rows (%d, %v)", n, err)
	}
}

func mustList(t *testing.T, h *HybridWorkflowStore) []*Workflow {
	t.Helper()
	all, err := h.ListAllProfilesWorkflows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := make([]*Workflow, len(all))
	for i := range all {
		out[i] = &all[i]
	}
	return out
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
