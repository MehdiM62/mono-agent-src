package org

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// notTheOperatorMarkers is every marker of what is not the operator's own
// terminal: an agent's shell, the chat's runs, a workflow's processes.
func notTheOperatorMarkers() []string {
	return append(append(append([]string{}, agentMarkersForTest...), notTheOperator...), "MONOAGENT_AGENT_CONTEXT")
}

// seedSendingGrants gives role of org two automations in the database at
// dbPath: send, whose workflow sends an email, and sum, whose doesn't.
func seedSendingGrants(t *testing.T, dbPath, org, role string) {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	wfs := workflow.NewSQLiteWorkflowStore(db.DB)
	for alias, node := range map[string]workflow.WorkflowNode{
		"send": {Type: "comm.email_send", Name: "Email", Config: map[string]interface{}{"to": "ada@example.test", "subject": "Hi", "body": "Hello"}},
		"sum":  {Type: "core.set", Name: "Set"},
	} {
		id := "wf-" + alias
		node.ID, node.WorkflowID = id+"-n", id
		if err := wfs.CreateWorkflow(ctx, &workflow.Workflow{ID: id, Name: id, ProfileID: "default"}); err != nil {
			t.Fatal(err)
		}
		if err := wfs.SaveWorkflowNodes(ctx, id, []workflow.WorkflowNode{node}); err != nil {
			t.Fatal(err)
		}
		if _, err := orggrant.NewStore(db.DB).UpsertGrant(ctx, orggrant.GrantInput{ProfileID: "default", OrgName: org, RoleID: role,
			Tool: orggrant.Tool{Alias: alias, WorkflowID: id, Tier: orgdesign.TierConsequential}}); err != nil {
			t.Fatal(err)
		}
	}
}

// `org approve` of a call of an automation that sends a message to a person
// is the user's own: an agent, the chat or a workflow is refused (exit 3)
// and nothing reaches monomind. Denying it, and approving anything else,
// stay theirs (#831 follow-up N1c).
func TestOrgApproveOfASendingAutomationIsTheOperatorsOnly(t *testing.T) {
	dir := t.TempDir()
	argsLog := filepath.Join(dir, "args.log")
	bin := filepath.Join(dir, "monomind")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+
		`if [ "$1" = "--version" ]; then echo '{"v":1,"version":"9.0.0","min_caller":"1.0.0","capabilities":["agent-exec","agent-scan","org-json-v1"]}'; exit 0; fi`+"\n"+
		`echo "$*" >> '`+argsLog+"'\necho '{\"v\":1,\"ok\":true}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(monomind.EnvOverride, bin)
	t.Setenv("HOME", t.TempDir())
	asOrgOperator(t)
	cfg := &testConfig{DBPath: filepath.Join(t.TempDir(), "o.db"), ProfileID: "default"}
	seedSendingGrants(t, cfg.DBPath, "growth", "writer")
	run := func(args ...string) error {
		t.Helper()
		var err error
		captureStdout(t, func() {
			cmd := newOrgCmd(cfg)
			cmd.SetArgs(args)
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			err = cmd.Execute()
		})
		return err
	}
	logged := func() string {
		b, _ := os.ReadFile(argsLog)
		return string(b)
	}

	for _, k := range notTheOperatorMarkers() {
		t.Setenv(k, "1")
		if err := run("approve", "growth", "writer", "monoagent__automation_send"); exitCodeFor(err) != 3 || !strings.Contains(err.Error(), "only the user approves") {
			t.Errorf("%s set: approve of a sending automation: %v", k, err)
		}
		for _, args := range [][]string{
			{"deny", "growth", "writer", "monoagent__automation_send"},
			{"approve", "growth", "writer", "monoagent__automation_sum"},
			{"approve", "growth", "writer", "Bash"},
		} {
			if err := run(args...); err != nil {
				t.Errorf("%s set: %v: %v", k, args, err)
			}
		}
		t.Setenv(k, "")
	}
	if strings.Contains(logged(), "approve growth writer monoagent__automation_send") {
		t.Fatalf("an approval of a sending automation reached monomind:\n%s", logged())
	}
	if err := run("approve", "growth", "writer", "monoagent__automation_send"); err != nil ||
		!strings.Contains(logged(), "approve growth writer monoagent__automation_send") {
		t.Fatalf("the operator's approval: %v\n%s", err, logged())
	}
}

// So is `org chat approve`, the app's and the chat's way to approve: an
// approval of a sending automation is refused to what is not the operator,
// with nothing sent; denying it and approving another stay open.
func TestOrgChatApproveOfASendingAutomationIsTheOperatorsOnly(t *testing.T) {
	cfg, fake, _ := setupOrgChat(t)
	asOrgOperator(t)
	seedSendingGrants(t, cfg.DBPath, "acme", "dev")
	fake.lists["approvals"], _ = json.Marshal(map[string]interface{}{"v": 1, "org": "acme", "items": []map[string]interface{}{
		{"roleId": "dev", "action": "monoagent__automation_send", "question": "send it", "ts": 1700, "approved": nil, "requestId": "req-send"},
		{"roleId": "dev", "action": "monoagent__automation_sum", "question": "sum it", "ts": 1701, "approved": nil, "requestId": "req-sum"},
	}})

	for _, k := range notTheOperatorMarkers() {
		t.Setenv(k, "1")
		fake.sent = nil
		if _, err := runOrgChat(t, cfg, "approve", "acme", "req-send"); exitCodeFor(err) != 3 || !strings.Contains(err.Error(), "only the user approves") {
			t.Errorf("%s set: chat approve of a sending automation: %v", k, err)
		}
		if len(fake.sent) != 0 {
			t.Errorf("%s set: sent %v", k, fake.sent)
		}
		for _, args := range [][]string{{"deny", "acme", "req-send"}, {"approve", "acme", "req-sum"}} {
			if _, err := runOrgChat(t, cfg, args...); err != nil {
				t.Errorf("%s set: chat %v: %v", k, args, err)
			}
		}
		t.Setenv(k, "")
	}
	fake.sent = nil
	if _, err := runOrgChat(t, cfg, "approve", "acme", "req-send"); err != nil || len(fake.sent) != 1 {
		t.Fatalf("the operator's approval: %v %v", err, fake.sent)
	}
}
