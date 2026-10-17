package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/ai/chat"
	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/storage"
)

// A workflow the chat builds is not the operator's: no command records it
// as theirs, an activation from the chat's run neither, and the operator's
// own `workflow activate` does — the review that makes its runs the
// operator's (#831 review C; the engine side is internal/workflow's
// TestRunsOfDefinitionsTheOperatorNeverApprovedAreAIRuns).
func TestAChatBuiltWorkflowIsTheOperatorsOnlyOnceTheyActivateIt(t *testing.T) {
	_, cfg := persistTestEnv(t)
	asOperatorProcess(t)
	db, err := storage.NewDatabase(cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	tools := chat.NewMonoagentTools(db.DB, "", "default")
	out, err := tools.Execute("create_workflow", `{"name":"From the chat"}`)
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID string `json:"workflow_id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.ID == "" {
		t.Fatalf("create_workflow: %s %v", out, err)
	}
	if _, err := tools.Execute("add_workflow_node", `{"workflow_id":"`+created.ID+`","node_type":"trigger.manual","name":"Start"}`); err != nil {
		t.Fatal(err)
	}
	approvals := func() int {
		t.Helper()
		var n int
		if err := db.DB.QueryRow(`SELECT COUNT(*) FROM workflow_approvals WHERE workflow_id = ?`, created.ID).Scan(&n); err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
		return n
	}
	if n := approvals(); n != 0 {
		t.Fatalf("the chat's workflow has %d approvals", n)
	}
	t.Setenv(browserctl.EnvAICaller, "chat")
	runWorkflowSubcmd(t, cfg, "activate", created.ID)
	t.Setenv(browserctl.EnvAICaller, "")
	if n := approvals(); n != 0 {
		t.Fatalf("an activation from the chat's run approved it (%d)", n)
	}
	runWorkflowSubcmd(t, cfg, "activate", created.ID)
	if n := approvals(); n != 1 {
		t.Fatalf("the operator's activation recorded %d approvals, want 1", n)
	}
}

// `workflow validate` takes publication.register's approval, an expression
// per item, as a field of the node (#831).
func TestWorkflowValidateKnowsTheRegisterApproval(t *testing.T) {
	_, cfg := persistTestEnv(t)
	file := filepath.Join(t.TempDir(), "wf.json")
	doc := `{"name":"Publish","nodes":[{"id":"t","type":"trigger.manual","name":"Start"},` +
		`{"id":"r","type":"publication.register","name":"Register","config":{"platform":"x","kind":"post","body":"hi","approval":"{{ $json.hil_id }}"}}],` +
		`"connections":[{"source":"t","target":"r"}]}`
	if err := os.WriteFile(file, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Valid  bool `json:"valid"`
		Issues []struct {
			Field, Code string
		} `json:"issues"`
	}
	out := runWorkflowSubcmd(t, cfg, "validate", "--file", file)
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("validate --json: %v\n%s", err, out)
	}
	if !rep.Valid {
		t.Errorf("invalid: %s", out)
	}
	for _, is := range rep.Issues {
		if is.Field == "approval" && is.Code == "unknown_field" {
			t.Errorf("approval is not a field of publication.register: %s", out)
		}
	}
}
