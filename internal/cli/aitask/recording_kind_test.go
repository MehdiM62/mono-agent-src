package aitask

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

const goodRecordingResult = `{"summary":"ok","automation":{"id":"acme-crm","name":"Acme"},
 "action":{"actionType":"create_contact","description":"d","sideEffects":"read","steps":[
   {"id":"open","type":"navigate","url":"https://app.acme-crm.com/new"},
   {"id":"email","type":"type","configKey":"c.email","intent":"the Email field","value":"{{email}}"}]},
 "selectors":{"c.email":{"intent":"Email","candidates":[{"css":"[data-testid=e]","score":0.9},{"text":"Email","score":0.5}]}}}`

// A draft folder submitted as context reaches the agent's prompt and context
// folder; the agent's result is checked by the kind's validators at `complete`.
func TestRecordingAnalysisSubmitThenCompleteThroughTheCLI(t *testing.T) {
	f := &tokenFake{fake: fake{cwd: t.TempDir()}}
	d := deps(t, func() aitask.Launcher { return f })

	// The task's context snapshot is a read-only folder: unlock it for cleanup.
	t.Cleanup(func() {
		_ = filepath.WalkDir(filepath.Dir(d.DBPath()), func(p string, e os.DirEntry, err error) error {
			if err == nil && e.IsDir() {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
	})
	draft := t.TempDir()
	if err := os.WriteFile(filepath.Join(draft, "selectors.json"), []byte(`{"c.email":{"intent":"Email"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, d, "submit", "--kind", "recording-analysis", "--goal", "improve recording rec-9", "--context", "document="+draft)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var task struct{ TaskID, Kind, Status string }
	if err := json.Unmarshal([]byte(out), &task); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if task.Kind != "recording-analysis" || task.Status != "running" {
		t.Fatalf("task = %+v", task)
	}
	db, err := d.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	stored, err := (&aitask.Store{DB: db.DB, Profile: "p1"}).Get(task.TaskID)
	db.Close()
	if err != nil || stored.Access != aitask.AccessQA {
		t.Fatalf("access = %q (%v): the kind's least-access default must apply", stored.Access, err)
	}
	for _, want := range []string{"improve recording rec-9", "Context folder", "aitask complete " + task.TaskID, "never write a credential", `"selectors"`} {
		if !strings.Contains(stored.Prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(f.cwd, ".monoagent", "context", task.TaskID, "document", filepath.Base(draft), "selectors.json")); err != nil {
		t.Fatalf("the draft is not in the context folder: %v", err)
	}

	t.Setenv(aitask.EnvToken, f.token)
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(strings.Replace(goodRecordingResult, `"configKey":"c.email"`, `"configKey":"c.missing"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = run(t, d, "complete", task.TaskID, "--result", bad)
	if clicore.ExitCodeFor(err) != 3 || !strings.Contains(out, `"code":"schema_invalid"`) || !strings.Contains(out, `c.missing`) {
		t.Fatalf("a result whose selector is undefined must be refused with the reason: %v %s", err, out)
	}
	good := filepath.Join(t.TempDir(), "good.json")
	if err := os.WriteFile(good, []byte(goodRecordingResult), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = run(t, d, "complete", task.TaskID, "--result", good)
	if err != nil || !strings.Contains(out, `"status":"complete"`) {
		t.Fatalf("%v %s", err, out)
	}
}
