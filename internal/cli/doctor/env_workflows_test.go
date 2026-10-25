package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/health"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

func workflowDoctorEnv(t *testing.T) (*health.Env, *storage.Database, *workflow.HybridWorkflowStore) {
	t.Helper()
	root := t.TempDir()
	db, err := storage.NewDatabase(filepath.Join(root, "monoagent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	files, err := workflow.NewWorkflowFileStore(filepath.Join(root, "workflows"))
	if err != nil {
		t.Fatal(err)
	}
	env := &health.Env{DataDir: root, DB: db.DB}
	addWorkflowPublicationHooks(env)
	return env, db, workflow.NewHybridWorkflowStore(files, workflow.NewSQLiteWorkflowStore(db.DB))
}

// The doctor row reads real change records and file drift, and its fix runs the recovery.
func TestWorkflowPublicationHooksReadAndRecover(t *testing.T) {
	ctx := context.Background()
	env, db, store := workflowDoctorEnv(t)
	info, err := env.WorkflowPublications(ctx)
	if err != nil || len(info.Open) != 0 || len(info.Unreadable) != 0 {
		t.Fatalf("clean tree: %+v %v", info, err)
	}
	wf := &workflow.Workflow{ID: "wf-d", Name: "d", ProfileID: "default",
		Nodes: []workflow.WorkflowNode{{ID: "d-trig", WorkflowID: "wf-d", Type: "trigger.manual", Name: "d-trig", Config: map[string]any{}}}}
	if err := store.SaveWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	// An abandoned change (its owner is gone) and an unparsable file.
	onDisk, err := os.ReadFile(filepath.Join(env.DataDir, "workflows", "wf-d.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(onDisk)
	if _, err := db.DB.Exec(`INSERT INTO workflow_publications
		(workflow_id, kind, state, owner_pid, started_at, origin, prev_sha, prev_bytes, prev_existed)
		VALUES ('wf-d', 'save', 'pending', 0, ?, 'apply-fix', ?, ?, 1)`,
		time.Now().UTC(), hex.EncodeToString(sum[:]), onDisk); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.DataDir, "workflows", "broken.json"), []byte(`{"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err = env.WorkflowPublications(ctx)
	if err != nil || len(info.Open) != 1 || !info.Open[0].OwnerGone || len(info.Unreadable) != 1 || info.Unreadable[0] != "broken" {
		t.Fatalf("seeded tree: %+v %v", info, err)
	}
	// Reading changed nothing.
	if again, _ := env.WorkflowPublications(ctx); len(again.Open) != 1 {
		t.Fatal("reading resolved the change")
	}
	if n, err := env.RecoverWorkflowPublications(ctx); err != nil || n != 1 {
		t.Fatalf("recover: %d %v", n, err)
	}
	if info, _ = env.WorkflowPublications(ctx); len(info.Open) != 0 {
		t.Fatalf("the change is still open: %+v", info.Open)
	}
}

// With no workflow folder the row has nothing to report and creates nothing.
func TestWorkflowPublicationHooksCreateNothing(t *testing.T) {
	env := &health.Env{DataDir: t.TempDir()}
	addWorkflowPublicationHooks(env)
	if info, err := env.WorkflowPublications(context.Background()); err != nil || len(info.Open) != 0 {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(env.DataDir, "workflows")); !os.IsNotExist(err) {
		t.Fatal("the folder was created")
	}
}
