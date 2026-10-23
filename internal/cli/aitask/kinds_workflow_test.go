package aitask

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// tokenFake is the fake launcher that also keeps the token it is given, so the
// test can answer as the task's own session.
type tokenFake struct {
	fake
	token string
}

func (f *tokenFake) Detach(_ context.Context, id, token string) error {
	f.token = token
	return f.fake.Detach(context.Background(), id, token)
}

// writeResult puts body in task id's own work folder, the one place a
// confined task may hand a result file back from.
func writeResult(t *testing.T, d *Deps, id, body string) string {
	t.Helper()
	work := filepath.Join(filepath.Dir(d.DBPath()), "aitask", id, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(work, "result.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func workflowResultJSON(trigger, node string) string {
	return `{"summary":"Fetches a URL.","validation":{"valid":true,"errors":[],"warnings":[]},"workflow":{"name":"Fetch","description":"GET","is_active":false,` +
		`"nodes":[{"id":"n1","type":"trigger.manual","name":"Start","config":{}},{"id":"n2","type":"http.request","name":"Fetch","config":` + node + `}],` +
		`"connections":[{"id":"c1","source":"n1","source_handle":"main","target":"n2","target_handle":"main"}]}}`
}

// `aitask submit --kind workflow-generate` makes the kind's first turn, and
// `aitask complete` (as the task's own session) refuses a workflow that
// fails `workflow validate` with the validator's reason, then accepts a valid one.
func TestSubmitWorkflowGenerateThenCompleteIsCheckedByTheValidator(t *testing.T) {
	f := &tokenFake{fake: fake{cwd: t.TempDir()}}
	d := deps(t, func() aitask.Launcher { return f })
	out, err := run(t, d, "submit", "--kind", "workflow-generate", "--goal", "fetch a url every morning")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var task struct {
		TaskID string `json:"taskId"`
		Kind   string `json:"kind"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out), &task); err != nil || task.Kind != "workflow-generate" || task.Status != "running" || f.token == "" {
		t.Fatalf("%v %s token=%q", err, out, f.token)
	}
	db, _ := d.OpenDB()
	defer db.Close()
	stored, err := (&aitask.Store{DB: db.DB, Profile: "p1"}).Get(task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Access != aitask.AccessWorkspace {
		t.Errorf("access = %q", stored.Access)
	}
	for _, want := range []string{"fetch a url every morning", "the same validator", "node schema", "aitask complete " + task.TaskID, `"workflow"`} {
		if !strings.Contains(stored.Prompt, want) {
			t.Errorf("the first turn lacks %q:\n%s", want, stored.Prompt)
		}
	}

	t.Setenv(aitask.EnvToken, f.token)
	// http.request without its url: the shape fits, the validator says no.
	refused, err := run(t, d, "complete", task.TaskID, "--result", writeResult(t, d, task.TaskID, workflowResultJSON("", `{"method":"GET"}`)))
	if clicore.ExitCodeFor(err) != 3 || !strings.Contains(refused, `"code":"schema_invalid"`) || !strings.Contains(refused, "workflow validate") {
		t.Fatalf("an invalid workflow must be refused (exit 3): %v %s", err, refused)
	}
	if got, _ := (&aitask.Store{DB: db.DB, Profile: "p1"}).Get(task.TaskID); got.Status != aitask.StatusRunning {
		t.Fatalf("a refused result changed the task: %s", got.Status)
	}
	accepted, err := run(t, d, "complete", task.TaskID, "--result", writeResult(t, d, task.TaskID, workflowResultJSON("", `{"method":"GET","url":"https://example.com/api"}`)))
	if err != nil || !strings.Contains(accepted, `"status":"complete"`) {
		t.Fatalf("a valid workflow is accepted: %v %s", err, accepted)
	}
}

func TestSubmitFailedRunFixThenCompleteRefusesAnEditThatNamesNoWorkflow(t *testing.T) {
	f := &tokenFake{fake: fake{cwd: t.TempDir()}}
	d := deps(t, func() aitask.Launcher { return f })
	out, err := run(t, d, "submit", "--kind", "failed-run-fix", "--goal", "Workflow wf-1 run ex-9 failed.")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var task struct {
		TaskID string `json:"taskId"`
	}
	_ = json.Unmarshal([]byte(out), &task)
	t.Setenv(aitask.EnvToken, f.token)
	bad := `{"summary":"s","diagnosis":"d","fix":{"kind":"workflow-edit","description":"x"},"changed":[],"reran":false}`
	refused, err := run(t, d, "complete", task.TaskID, "--result", writeResult(t, d, task.TaskID, bad))
	if clicore.ExitCodeFor(err) != 3 || !strings.Contains(refused, "changed is empty") {
		t.Fatalf("%v %s", err, refused)
	}
	good := `{"summary":"s","diagnosis":"d","fix":{"kind":"workflow-edit","description":"x"},"changed":["wf-1"],"reran":false}`
	if accepted, err := run(t, d, "complete", task.TaskID, "--result", writeResult(t, d, task.TaskID, good)); err != nil || !strings.Contains(accepted, `"status":"complete"`) {
		t.Fatalf("%v %s", err, accepted)
	}
}
