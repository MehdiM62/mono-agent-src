package workflow

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// versionWF is trigger.manual → core.set with set's value v.
func versionWF(id, v string) *Workflow {
	return &Workflow{
		ID: id, Name: id, ProfileID: "default",
		Nodes: []WorkflowNode{
			{ID: id + "-t", Type: "trigger.manual", Name: "T", Config: map[string]interface{}{}},
			{ID: id + "-s", Type: "core.set", Name: "S", Config: map[string]interface{}{"value": v}},
		},
		Connections: []WorkflowConnection{
			{ID: id + "-c", SourceNodeID: id + "-t", SourceHandle: "main", TargetNodeID: id + "-s", TargetHandle: "main"},
		},
	}
}

func versionNumbers(t *testing.T, h *HybridWorkflowStore, id string) []int {
	t.Helper()
	vs, err := h.ListWorkflowVersions(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	for _, v := range vs {
		out = append(out, v.Version)
	}
	return out
}

func setValue(t *testing.T, wf *Workflow) string {
	t.Helper()
	for _, n := range wf.Nodes {
		if n.Type == "core.set" {
			s, _ := n.Config["value"].(string)
			return s
		}
	}
	t.Fatalf("no core.set node in %+v", wf.Nodes)
	return ""
}

// Saves record a version only when the definition changed, and the
// workflow's version follows the history in the file and in SQLite.
func TestVersions_RecordedOnSaveAndDeduped(t *testing.T) {
	ctx := context.Background()
	h, s := newHybridForTest(t)

	w := versionWF("wf-v", "one")
	if err := h.CreateWorkflow(ctx, w); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveWorkflow(ctx, versionWF("wf-v", "one")); err != nil {
		t.Fatal(err)
	}
	if got := versionNumbers(t, h, "wf-v"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("after identical save: versions %v, want [1]", got)
	}

	w2 := versionWF("wf-v", "two")
	if err := h.SaveWorkflow(ctx, w2); err != nil {
		t.Fatal(err)
	}
	if w2.Version != 2 {
		t.Errorf("saved version = %d, want 2", w2.Version)
	}
	if got := versionNumbers(t, h, "wf-v"); len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("versions %v, want [2 1]", got)
	}
	fromFile, _ := h.GetWorkflow(ctx, "wf-v")
	fromSQL, _ := s.GetWorkflow(ctx, "wf-v")
	if fromFile.Version != 2 || fromSQL.Version != 2 {
		t.Errorf("file version %d, SQLite version %d, want 2", fromFile.Version, fromSQL.Version)
	}

	v1, err := h.GetWorkflowVersion(ctx, "wf-v", 1)
	if err != nil || v1 == nil {
		t.Fatalf("version 1: %v, %v", v1, err)
	}
	if got := setValue(t, v1.Snapshot.Apply(fromFile)); got != "one" {
		t.Errorf("version 1 snapshot value %q, want one", got)
	}
	if missing, err := h.GetWorkflowVersion(ctx, "wf-v", 9); err != nil || missing != nil {
		t.Errorf("missing version = %v, %v", missing, err)
	}
}

// A node edit outside a full save is captured as a version too.
func TestVersions_NodeEditCaptured(t *testing.T) {
	ctx := context.Background()
	h, _ := newHybridForTest(t)
	if err := h.CreateWorkflow(ctx, versionWF("wf-n", "one")); err != nil {
		t.Fatal(err)
	}
	wf, _ := h.GetWorkflow(ctx, "wf-n")
	wf.Nodes[1].Config = map[string]interface{}{"value": "edited"}
	if err := h.SaveWorkflowNodes(ctx, "wf-n", wf.Nodes); err != nil {
		t.Fatal(err)
	}
	if got := versionNumbers(t, h, "wf-n"); len(got) != 2 || got[0] != 2 {
		t.Fatalf("versions %v, want [2 1]", got)
	}
	if again, _ := h.GetWorkflow(ctx, "wf-n"); again.Version != 2 {
		t.Errorf("file version %d, want 2", again.Version)
	}
}

// An execution is pinned to the version current when it was created.
func TestVersions_ExecutionPinned(t *testing.T) {
	ctx := context.Background()
	h, _ := newHybridForTest(t)
	if err := h.CreateWorkflow(ctx, versionWF("wf-e", "one")); err != nil {
		t.Fatal(err)
	}
	exec := &WorkflowExecution{WorkflowID: "wf-e", TriggerType: "manual", ProfileID: "default"}
	if err := h.CreateExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}
	if err := h.SaveWorkflow(ctx, versionWF("wf-e", "two")); err != nil {
		t.Fatal(err)
	}
	got, err := h.ExecutionWorkflowVersion(ctx, exec.ID)
	if err != nil || got != 1 {
		t.Fatalf("pinned version = %d, %v; want 1", got, err)
	}
	later := &WorkflowExecution{WorkflowID: "wf-e", TriggerType: "manual", ProfileID: "default"}
	if err := h.CreateExecution(ctx, later); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.ExecutionWorkflowVersion(ctx, later.ID); got != 2 {
		t.Errorf("new execution pinned to %d, want 2", got)
	}
}

// A workflow saved before versions existed gets its first snapshot under its
// current number, without inventing a change.
func TestVersions_LegacyWorkflowCapturedAtCurrentVersion(t *testing.T) {
	ctx := context.Background()
	h, s := newHybridForTest(t)
	if err := h.CreateWorkflow(ctx, versionWF("wf-l", "one")); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-#21 workflow: no history, version 4.
	if _, err := s.db.Exec(`DELETE FROM workflow_versions WHERE workflow_id = 'wf-l'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE workflows SET version = 4 WHERE id = 'wf-l'`); err != nil {
		t.Fatal(err)
	}
	if v, err := h.CaptureWorkflowVersion(ctx, "wf-l"); err != nil || v != 4 {
		t.Fatalf("captured version %d, %v; want 4", v, err)
	}
	if err := h.SaveWorkflow(ctx, versionWF("wf-l", "two")); err != nil {
		t.Fatal(err)
	}
	if got := versionNumbers(t, h, "wf-l"); len(got) != 2 || got[0] != 5 || got[1] != 4 {
		t.Errorf("versions %v, want [5 4]", got)
	}
}

// Prune keeps the newest N and any version an unfinished run is pinned to.
func TestVersions_PruneSparesPinned(t *testing.T) {
	ctx := context.Background()
	h, s := newHybridForTest(t)
	if err := h.CreateWorkflow(ctx, versionWF("wf-p", "v1")); err != nil {
		t.Fatal(err)
	}
	waiting := &WorkflowExecution{WorkflowID: "wf-p", TriggerType: "manual", ProfileID: "default"}
	if err := h.CreateExecution(ctx, waiting); err != nil {
		t.Fatal(err)
	}
	if err := s.SetExecutionWaiting(ctx, waiting.ID, `{}`); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"v2", "v3", "v4"} {
		if err := h.SaveWorkflow(ctx, versionWF("wf-p", v)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := h.PruneWorkflowVersions(ctx, "wf-p", 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1 (version 2)", n)
	}
	if got := versionNumbers(t, h, "wf-p"); len(got) != 3 || got[0] != 4 || got[1] != 3 || got[2] != 1 {
		t.Errorf("versions %v, want [4 3 1]", got)
	}
	if _, err := h.PruneWorkflowVersions(ctx, "wf-p", 0); err == nil {
		t.Error("keep 0 accepted")
	}
}

// A save whose SQLite half fails (a connection naming a node that does not
// exist, discovered after the row and nodes were written) leaves both stores
// with the previous definition and records no version.
func TestVersions_SaveIsAllOrNothing_SQLFailure(t *testing.T) {
	ctx := context.Background()
	h, s := newHybridForTest(t)
	if err := h.CreateWorkflow(ctx, versionWF("wf-a", "old")); err != nil {
		t.Fatal(err)
	}
	bad := versionWF("wf-a", "new")
	bad.Name = "renamed"
	bad.Connections[0].TargetNodeID = "nope"
	if err := h.SaveWorkflow(ctx, bad); err == nil {
		t.Fatal("save with a dangling connection succeeded")
	}
	assertOldDefinition(t, h, s, "wf-a")
}

// A save whose file write fails leaves SQLite untouched.
func TestVersions_SaveIsAllOrNothing_FileFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write-permission removal behaves differently on Windows")
	}
	ctx := context.Background()
	dir := t.TempDir()
	files, err := NewWorkflowFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newMigratedStore(t)
	h := NewHybridWorkflowStore(files, s)
	if err := h.CreateWorkflow(ctx, versionWF("wf-f", "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if os.WriteFile(filepath.Join(dir, "probe"), nil, 0o644) == nil {
		t.Skip("directory still writable (running as root?)")
	}
	next := versionWF("wf-f", "new")
	next.Name = "renamed"
	if err := h.SaveWorkflow(ctx, next); err == nil {
		t.Fatal("save into a read-only directory succeeded")
	}
	assertOldDefinition(t, h, s, "wf-f")
}

func assertOldDefinition(t *testing.T, h *HybridWorkflowStore, s *SQLiteWorkflowStore, id string) {
	t.Helper()
	ctx := context.Background()
	fromFile, _ := h.GetWorkflow(ctx, id)
	fromSQL, _ := s.GetWorkflow(ctx, id)
	for name, wf := range map[string]*Workflow{"file": fromFile, "sqlite": fromSQL} {
		if wf == nil {
			t.Fatalf("%s: workflow gone", name)
		}
		if wf.Name != id || setValue(t, wf) != "old" || len(wf.Connections) != 1 || wf.Version != 1 {
			t.Errorf("%s: name %q value %q conns %d version %d; want the old definition",
				name, wf.Name, setValue(t, wf), len(wf.Connections), wf.Version)
		}
	}
	if got := versionNumbers(t, h, id); len(got) != 1 {
		t.Errorf("versions %v, want only [1]", got)
	}
}
