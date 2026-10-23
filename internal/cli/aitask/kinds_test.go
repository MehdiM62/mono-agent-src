package aitask

import (
	"encoding/json"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	_ "github.com/monoes/mono-agent/internal/aitaskkinds"
)

// Each kind of internal/aitaskkinds submits through the fake launcher and is
// stored with the access level its kind asks for.
func TestSubmitCreatesTheNewKindsAtWorkspaceAccess(t *testing.T) {
	for _, kind := range []string{"workflow-generate", "recording-analyze", "failed-run-fix"} {
		d := deps(t, func() aitask.Launcher { return &fake{cwd: t.TempDir()} })
		out, err := run(t, d, "submit", "--kind", kind, "--goal", "x", "--queue-only")
		if err != nil {
			t.Fatalf("%s: %v %s", kind, err, out)
		}
		var task aitask.Task
		if err := json.Unmarshal([]byte(out), &task); err != nil || task.Kind != kind || task.Status != "queued" {
			t.Fatalf("%s: %v %s", kind, err, out)
		}
		db, _ := d.OpenDB()
		got, err := (&aitask.Store{DB: db.DB, Profile: "p1"}).Get(task.ID)
		db.Close()
		if err != nil || got.Access != aitask.AccessWorkspace || got.Unattended {
			t.Fatalf("%s: access %q unattended %v (%v)", kind, got.Access, got.Unattended, err)
		}
	}
}
