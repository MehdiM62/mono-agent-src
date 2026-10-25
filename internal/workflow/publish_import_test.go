package workflow

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// handEdit changes the workflow's file the way an editor does: the store is not involved.
func handEdit(t *testing.T, h *HybridWorkflowStore, id string, edit func(w *Workflow)) {
	t.Helper()
	w, err := h.files.GetWorkflow(context.Background(), id)
	if err != nil || w == nil {
		t.Fatalf("%v", err)
	}
	edit(w)
	if err := h.files.SaveWorkflow(context.Background(), w); err != nil {
		t.Fatal(err)
	}
}

func versionCount(t *testing.T, s *SQLiteWorkflowStore, id string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM workflow_versions WHERE workflow_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestImportFollowsAnOutsideEditAndRecordsIt(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if res, err := h.ImportFile(ctx, "wf-g", nil); err != nil || res.Imported {
		t.Fatalf("the first look at an unchanged file imported: %+v %v", res, err)
	}
	v0 := versionCount(t, s, "wf-g")
	handEdit(t, h, "wf-g", func(w *Workflow) { w.Nodes[1].Config = map[string]interface{}{"k": "by-hand"} })
	onDisk := fileBytes(t, h, "wf-g")
	res, err := h.ImportFile(ctx, "wf-g", nil)
	if err != nil || !res.Imported || res.Created {
		t.Fatalf("%+v %v", res, err)
	}
	if sqlConfig(t, s, "wf-g", "g-set") != "by-hand" {
		t.Fatal("the rows did not follow the file")
	}
	if versionCount(t, s, "wf-g") != v0+1 {
		t.Fatal("the edit was not recorded as a version")
	}
	var note string
	_ = s.db.QueryRow(`SELECT note FROM workflow_versions WHERE workflow_id = 'wf-g' ORDER BY version DESC LIMIT 1`).Scan(&note)
	if note != ImportNote {
		t.Fatalf("note %q", note)
	}
	if !bytes.Equal(onDisk, fileBytes(t, h, "wf-g")) {
		t.Fatal("the import rewrote the file")
	}
	// The edited definition has no approval of its own; the earlier approval is still there.
	cur, _ := h.GetWorkflow(ctx, "wf-g")
	hash, _ := definitionHash(cur)
	if ok, _ := h.DefinitionApproved(ctx, "wf-g", hash); ok || approvals(t, s, "wf-g") != 1 {
		t.Fatalf("approvals after an outside edit: approved=%v rows=%d", ok, approvals(t, s, "wf-g"))
	}
	// Seen again, nothing more happens.
	if res, _ := h.ImportFile(ctx, "wf-g", nil); res.Imported || versionCount(t, s, "wf-g") != v0+1 {
		t.Fatal("the same file was imported twice")
	}
}

func TestImportOfAReformattedFileChangesNothing(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	v0 := versionCount(t, s, "wf-g")
	handEdit(t, h, "wf-g", func(w *Workflow) { w.UpdatedAt = w.UpdatedAt.Add(time.Hour) })
	if res, err := h.ImportFile(ctx, "wf-g", nil); err != nil || res.Imported {
		t.Fatalf("%+v %v", res, err)
	}
	if versionCount(t, s, "wf-g") != v0 {
		t.Fatal("a version was recorded for an unchanged definition")
	}
	if n, _ := rowsCount(s, `SELECT COUNT(*) FROM workflow_file_state WHERE workflow_id = 'wf-g'`); n != 1 {
		t.Fatal("the new bytes were not remembered")
	}
}

func rowsCount(s *SQLiteWorkflowStore, q string) (int, error) {
	var n int
	err := s.db.QueryRow(q).Scan(&n)
	return n, err
}

// A file whose change is open is not imported: this is the importer reading a save that has not
// committed.
func TestImportSkipsAWorkflowWithAChangeOpen(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	handEdit(t, h, "wf-g", func(w *Workflow) { w.Nodes[1].Config = map[string]interface{}{"k": "pending"} })
	if _, err := s.db.Exec(`INSERT INTO workflow_publications
		(workflow_id, kind, state, owner_pid, started_at) VALUES ('wf-g', 'save', 'pending', ?, ?)`,
		os.Getpid(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if res, err := h.ImportFile(ctx, "wf-g", nil); err != nil || res.Imported {
		t.Fatalf("%+v %v", res, err)
	}
	if sqlConfig(t, s, "wf-g", "g-set") == "pending" {
		t.Fatal("an uncommitted file reached the rows")
	}
}

func TestImportIgnoresAFileThatDoesNotParse(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if err := os.WriteFile(filepath.Join(h.files.dir, "wf-g.json"), []byte(`{"id":"wf-g","nodes":[{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if res, err := h.ImportFile(ctx, "wf-g", nil); err != nil || res.Imported {
		t.Fatalf("%+v %v", res, err)
	}
	if sqlConfig(t, s, "wf-g", "g-set") != "v" {
		t.Fatal("rows changed")
	}
}

func TestImportCreatesRowsForAFileThatHasNone(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if err := h.files.SaveWorkflow(ctx, mirrorWF("wf-file-only", "f-trig", "f-set")); err != nil {
		t.Fatal(err)
	}
	res, err := h.ImportFile(ctx, "wf-file-only", nil)
	if err != nil || !res.Imported || !res.Created {
		t.Fatalf("%+v %v", res, err)
	}
	if got, _ := s.GetWorkflow(ctx, "wf-file-only"); got == nil || len(got.Nodes) != 2 {
		t.Fatalf("rows: %+v", got)
	}
}

func TestImportRefusesIdsOwnedByAnotherWorkflow(t *testing.T) {
	ctx := context.Background()
	h, _, _, _ := guardedRig(t)
	clash := mirrorWF("wf-clash", "g-trig", "c-set")
	clash.Connections = nil
	if err := h.files.SaveWorkflow(ctx, clash); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ImportFile(ctx, "wf-clash", nil); !errors.Is(err, ErrSharedIDs) {
		t.Fatalf("%v", err)
	}
}

func TestImportHonoursTheCallersRefusal(t *testing.T) {
	ctx := context.Background()
	h, s, _, _ := guardedRig(t)
	if err := h.files.SaveWorkflow(ctx, mirrorWF("wf-refused", "r-trig", "r-set")); err != nil {
		t.Fatal(err)
	}
	res, err := h.ImportFile(ctx, "wf-refused", func(*Workflow) bool { return false })
	if err != nil || res.Imported {
		t.Fatalf("%+v %v", res, err)
	}
	if got, _ := s.GetWorkflow(ctx, "wf-refused"); got != nil {
		t.Fatal("a refused file was imported")
	}
}

// The importer, run inside a save's window (file replaced, commit not yet made), imports nothing; the
// commit then fails and the rows never held the proposed content.
func TestImportInsideAFailingSavesWindowNeverSeesTheProposal(t *testing.T) {
	ctx := context.Background()
	h, s, hash, nodes := guardedRig(t)
	var inWindow ImportResult
	var importErr error
	guardedCommitFault = func() error {
		inWindow, importErr = h.ImportFile(ctx, "wf-g", nil)
		return errors.New("commit failed")
	}
	if err := h.SaveNodesGuarded(ctx, "wf-g", nodes, SaveGuard{ExpectHash: hash}); err == nil {
		t.Fatal("the save should have failed")
	}
	if importErr != nil || inWindow.Imported {
		t.Fatalf("the importer acted inside the window: %+v %v", inWindow, importErr)
	}
	if sqlConfig(t, s, "wf-g", "g-set") == "edited" {
		t.Fatal("the rows hold content that never committed")
	}
	if again, err := h.ImportFile(ctx, "wf-g", nil); err != nil || again.Imported {
		t.Fatalf("after the rollback: %+v %v", again, err)
	}
}
