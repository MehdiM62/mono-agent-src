package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai/generate"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/storage"
)

const genValid = "```json\n" + `{"name":"Fetch feed","nodes":[
 {"id":"n1","type":"trigger.manual","name":"Start","position":{"x":100,"y":100},"config":{}},
 {"id":"n2","type":"http.request","name":"Fetch","position":{"x":350,"y":100},"config":{"url":"https://example.com"}}],
 "connections":[{"id":"c1","source":"n1","source_handle":"main","target":"n2","target_handle":"main"}]}` + "\n```"

const genNoTrigger = `{"name":"Fetch feed","nodes":[
 {"id":"n2","type":"http.request","name":"Fetch","config":{}}],"connections":[]}`

// fakeGenerateExec makes every turn answer from answers, in order (the
// last one repeats), and counts the turns.
func fakeGenerateExec(t *testing.T, answers ...string) *int {
	t.Helper()
	turns := 0
	orig := generateExecFor
	generateExecFor = func(_ context.Context, runtime, _ string) (generate.ExecFunc, error) {
		if runtime != "fake" {
			t.Errorf("runtime = %q, want fake", runtime)
		}
		return func(_ context.Context, prompt, system string) (string, error) {
			if !strings.Contains(system, "http.request") || !strings.Contains(system, "trigger.manual") {
				t.Error("system prompt lacks the node catalog")
			}
			i := turns
			if i >= len(answers) {
				i = len(answers) - 1
			}
			turns++
			return answers[i], nil
		}, nil
	}
	t.Cleanup(func() { generateExecFor = orig })
	return &turns
}

func TestWorkflowGenerateSavesInactiveDraftAfterBadJSON(t *testing.T) {
	home, cfg := persistTestEnv(t)
	turns := fakeGenerateExec(t, "here you go: {broken", genValid)
	out, err := runWorkflowCmdIn(t, cfg, "", "generate", "fetch", "example.com", "--runtime", "fake")
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
	if _, err := runWorkflowCmdIn(t, cfg, "", "validate", got.ID); err != nil {
		t.Errorf("saved draft does not validate: %v", err)
	}
}

func TestWorkflowGenerateRetriesValidationErrorsNoSave(t *testing.T) {
	_, cfg := persistTestEnv(t)
	cfg.JSONOutput = false
	turns := fakeGenerateExec(t, genNoTrigger, genValid)
	out, err := runWorkflowCmdIn(t, cfg, "", "generate", "fetch example.com", "--runtime", "fake", "--no-save", "--name", "Mine")
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
	ws, err := runWorkflowCmdIn(t, &globalConfig{DBPath: cfg.DBPath, JSONOutput: true, ProfileID: "default"}, "", "list")
	if err != nil || strings.Contains(ws, "Mine") {
		t.Errorf("--no-save saved something: %s %v", ws, err)
	}
}

func TestWorkflowGenerateGivesUpAfterMaxAttempts(t *testing.T) {
	_, cfg := persistTestEnv(t)
	turns := fakeGenerateExec(t, genNoTrigger)
	out, err := runWorkflowCmdIn(t, cfg, "", "generate", "fetch", "--runtime", "fake", "--max-attempts", "2")
	var ce *cliError
	if !errors.As(err, &ce) || ce.code != 3 {
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
	_, cfg := persistTestEnv(t)
	orig := generateScan
	generateScan = func(context.Context) (*monomind.ScanResult, error) { return &monomind.ScanResult{}, nil }
	t.Cleanup(func() { generateScan = orig })
	_, err := runWorkflowCmdIn(t, cfg, "", "generate", "anything")
	if err == nil || !strings.Contains(err.Error(), "agent install") || !strings.Contains(err.Error(), "--runtime") {
		t.Fatalf("err = %v", err)
	}
}
