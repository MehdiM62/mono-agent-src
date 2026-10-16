package profile

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// asOperator makes this test process the user's own terminal.
func asOperator(t *testing.T) {
	t.Helper()
	for _, k := range operator.MarkerEnv() {
		t.Setenv(k, "")
	}
}

// A profile's folder is a trusted zone for runs of workflows the user never
// approved, so only the user sets it (create --root-dir) or moves it; an
// agent's shell, the chat's run and a workflow run are refused and nothing
// changes. A plain create and move --check stay open (#831 S1).
func TestProfileFolderIsTheUsers(t *testing.T) {
	for _, marker := range []string{"CLAUDECODE", browserctl.EnvAICaller, workflow.EnvWorkflowRun} {
		t.Run(marker, func(t *testing.T) {
			dbPath := newProfileDocsCLITestDB(t)
			home := t.TempDir()
			t.Setenv(marker, "1")

			_, err := runProfileJSON(t, dbPath, "create", "evil", "--root-dir", home)
			if err == nil || exitCodeOf(t, err) != 3 || !strings.Contains(err.Error(), "only the user sets or moves a profile's folder") || !strings.Contains(err.Error(), marker) {
				t.Fatalf("create --root-dir as %s: %v", marker, err)
			}
			_, err = runProfileJSON(t, dbPath, "move", "default", home)
			if err == nil || exitCodeOf(t, err) != 3 || !strings.Contains(err.Error(), marker) {
				t.Fatalf("move as %s: %v", marker, err)
			}

			db, err := storage.NewDatabase(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var n int
			db.DB.QueryRow(`SELECT COUNT(*) FROM profiles WHERE name = 'evil' OR root_dir != ''`).Scan(&n)
			if n != 0 {
				t.Fatalf("a refused command changed %d profile(s)", n)
			}
			if got := profiledir.Root(db.DB, "default"); got == home {
				t.Fatalf("the default profile moved to %s", got)
			}

			var created profileCreateResult
			mustProfileJSON(t, dbPath, &created, "create", "Plain")
			if created.RootDir == home || created.RootDir == "" {
				t.Fatalf("a plain create as %s: %+v", marker, created)
			}
			var checked map[string]interface{}
			mustProfileJSON(t, dbPath, &checked, "move", "default", filepath.Join(home, "x"), "--check")
			if checked["moved"] != false {
				t.Fatalf("--check as %s: %+v", marker, checked)
			}
		})
	}

	dbPath := newProfileDocsCLITestDB(t)
	custom := filepath.Join(t.TempDir(), "work")
	var created profileCreateResult
	mustProfileJSON(t, dbPath, &created, "create", "Work", "--root-dir", custom)
	if created.RootDir != custom {
		t.Fatalf("the user's create --root-dir: %+v", created)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	var res map[string]interface{}
	mustProfileJSON(t, dbPath, &res, "move", "Work", moved)
	if res["moved"] != true || res["root_dir"] != moved {
		t.Fatalf("the user's move: %+v", res)
	}
}
