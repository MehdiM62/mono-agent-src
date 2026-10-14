package workflowgenerate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai/generate"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/storage"
)

const genValid = "```json\n" + `{"name":"Fetch feed","nodes":[
 {"id":"n1","type":"trigger.manual","name":"Start","position":{"x":100,"y":100},"config":{}},
 {"id":"n2","type":"http.request","name":"Fetch","position":{"x":350,"y":100},"config":{"url":"https://example.com"}}],
 "connections":[{"id":"c1","source":"n1","source_handle":"main","target":"n2","target_handle":"main"}]}` + "\n```"

const genNoTrigger = `{"name":"Fetch feed","nodes":[
 {"id":"n2","type":"http.request","name":"Fetch","config":{}}],"connections":[]}`

// fakeExec is an agent whose every turn answers from answers, in order (the last one
// repeats); turns counts them.
func fakeExec(t *testing.T, turns *int, answers ...string) func(context.Context, string, string) (generate.ExecFunc, error) {
	t.Helper()
	return func(_ context.Context, runtime, _ string) (generate.ExecFunc, error) {
		if runtime != "fake" {
			t.Errorf("runtime = %q, want fake", runtime)
		}
		return func(_ context.Context, prompt, system string) (string, error) {
			if !strings.Contains(system, "http.request") || !strings.Contains(system, "trigger.manual") {
				t.Error("system prompt lacks the node catalog")
			}
			i := *turns
			if i >= len(answers) {
				i = len(answers) - 1
			}
			*turns++
			return answers[i], nil
		}, nil
	}
}

func TestWorkflowGenerateSavesInactiveDraftAfterBadJSON(t *testing.T) {
	t.Parallel()
	home, cfg := persistTestEnv(t)
	turns := new(int)
	d := testDeps(cfg, fakeExec(t, turns, "here you go: {broken", genValid), nil)
	out, err := runGenerate(t, d, "fetch", "example.com", "--runtime", "fake")
	if err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}
	var got generateOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if *turns != 2 || got.Attempts != 2 || !got.Saved || got.ID == "" || got.NodeCount != 2 || !got.Valid || got.IsActive {
		t.Fatalf("output = %+v (turns %d)", got, *turns)
	}
	if _, err := os.Stat(filepath.Join(home, ".monoagent", "workflows", got.ID+".json")); err != nil {
		t.Fatalf("no workflow file: %v", err)
	}
	db, err := storage.NewDatabase(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var owner string
	var active bool
	if err := db.DB.QueryRow(`SELECT profile_id, is_active FROM workflows WHERE id = ?`, got.ID).Scan(&owner, &active); err != nil || owner != "default" || active {
		t.Fatalf("SQLite row = %q active=%v, %v", owner, active, err)
	}
	// The model's short ids were replaced, consistently.
	if got.Workflow.Nodes[0].ID == "n1" || got.Workflow.Connections[0].Source != got.Workflow.Nodes[0].ID {
		t.Errorf("saved ids = %+v", got.Workflow)
	}
	saved, err := testStore(cfg)(db).GetWorkflow(context.Background(), got.ID)
	if err != nil || saved == nil {
		t.Fatalf("saved draft: %v", err)
	}
	if errs, _ := generate.Validator(testRegistry(nil).Has)(saved); len(errs) != 0 {
		t.Errorf("saved draft does not validate: %v", errs)
	}
}

func TestWorkflowGenerateRetriesValidationErrorsNoSave(t *testing.T) {
	t.Parallel()
	home, cfg := persistTestEnv(t)
	cfg.JSONOutput = false
	turns := new(int)
	d := testDeps(cfg, fakeExec(t, turns, genNoTrigger, genValid), nil)
	out, err := runGenerate(t, d, "fetch example.com", "--runtime", "fake", "--no-save", "--name", "Mine")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if *turns != 2 {
		t.Fatalf("turns = %d", *turns)
	}
	var doc struct {
		Name     string `json:"name"`
		IsActive bool   `json:"is_active"`
		Nodes    []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("--no-save output is not the document: %v\n%s", err, out)
	}
	if doc.Name != "Mine" || doc.IsActive || len(doc.Nodes) != 2 || doc.Nodes[0].ID != "n1" {
		t.Errorf("document = %+v", doc)
	}
	db, err := openTestDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM workflows`).Scan(&n); err != nil || n != 0 {
		t.Errorf("--no-save saved %d workflow(s) (%v)", n, err)
	}
	if files, _ := os.ReadDir(filepath.Join(home, ".monoagent", "workflows")); len(files) != 0 {
		t.Errorf("--no-save wrote %d workflow file(s)", len(files))
	}
}

func TestWorkflowGenerateGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	_, cfg := persistTestEnv(t)
	turns := new(int)
	d := testDeps(cfg, fakeExec(t, turns, genNoTrigger), nil)
	out, err := runGenerate(t, d, "fetch", "--runtime", "fake", "--max-attempts", "2")
	var ce *clicore.Error
	if !errors.As(err, &ce) || ce.Code != 3 {
		t.Fatalf("err = %v, want exit 3", err)
	}
	if *turns != 2 {
		t.Fatalf("turns = %d, want 2", *turns)
	}
	var body map[string]any
	if json.Unmarshal([]byte(out), &body) != nil || body["code"] != "not_valid" || body["attempts"] != float64(2) {
		t.Errorf("json error = %s", out)
	}
	if !strings.Contains(err.Error(), "2 attempt(s)") || !strings.Contains(err.Error(), "trigger") {
		t.Errorf("error = %q", err)
	}
}

func TestWorkflowGenerateAutoWithoutRuntimeSaysHowToSetUp(t *testing.T) {
	t.Parallel()
	_, cfg := persistTestEnv(t)
	d := testDeps(cfg, nil, func(context.Context) (*monomind.ScanResult, error) { return &monomind.ScanResult{}, nil })
	_, err := runGenerate(t, d, "anything")
	if err == nil || !strings.Contains(err.Error(), "agent install") || !strings.Contains(err.Error(), "--runtime") {
		t.Fatalf("err = %v", err)
	}
}
