package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Only the operator's own terminal decides what agents asked for: the chat's
// processes and anything a workflow runs (which a chat or an org can build)
// are refused, whatever other markers they lack (C9, #820).
func TestOnlyTheOperatorDecidesAgentRequests(t *testing.T) {
	cfg := sendEnv(t)
	for _, k := range []string{browserctl.EnvAICaller, workflow.EnvWorkflowRun, browserctl.EnvAgentKind} {
		t.Setenv(k, "")
	}
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	site, err := browserctl.AskSite(ctx, db, "default", browserctl.KindChat, "https://shop.example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	held, err := browserctl.CreateSubmitItem(ctx, db, browserctl.SubmitRequest{Profile: "default", URL: "https://shop.example.com/cart",
		Form: browserctl.FormSummary{Action: "https://shop.example.com/pay", Method: "post", Submitter: "#pay"}})
	if err != nil {
		t.Fatal(err)
	}
	status := func(id string) string {
		var s string
		if err := db.QueryRow(`SELECT status FROM hil_pending WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	run := func(args ...string) error {
		var err error
		captureStdout(t, func() {
			root := newRootCmd()
			root.SetArgs(append([]string{"--db-path", cfg.DBPath, "--profile", "default", "--json"}, args...))
			err = root.Execute()
		})
		return err
	}

	for name, env := range map[string][2]string{
		"a chat tool call's run":         {browserctl.EnvAICaller, "chat"},
		"a process a workflow node runs": {workflow.EnvWorkflowRun, "exec-7"},
	} {
		t.Setenv(env[0], env[1])
		for _, args := range [][]string{
			{"hil", "approve", site}, {"hil", "reject", site}, {"hil", "approve", held.ID}, {"hil", "reject", held.ID},
			{"browser", "permissions", "set", "chat", "full-no-approvals"}, {"browser", "permissions", "site", "https://shop.example.com", "allow"},
			{"browser", "allow", "add", "https://shop.example.com"},
		} {
			if err := run(args...); err == nil || !strings.Contains(err.Error(), env[0]+" is set") && !strings.Contains(err.Error(), "an agent may not") {
				t.Errorf("%s: %v must be refused, got %v", name, args, err)
			}
		}
		if status(site) != "pending" || status(held.ID) != "pending" {
			t.Fatalf("%s decided an item: site %s, submit %s", name, status(site), status(held.ID))
		}
		t.Setenv(env[0], "")
	}
	if p, err := browserctl.LoadPermissions(db); err != nil || p.Level(browserctl.KindChat) != browserctl.DefaultLevels[browserctl.KindChat] || len(p.Sites) != 0 {
		t.Fatalf("the permissions changed: %+v %v", p, err)
	}

	// The operator's own terminal still decides.
	if err := run("hil", "approve", site); err != nil || status(site) != "approved" {
		t.Fatalf("the operator approves the site: %v %s", err, status(site))
	}
	if p, _ := browserctl.LoadPermissions(db); p.Rule("https://shop.example.com") != browserctl.RuleAllow {
		t.Errorf("approving the site adds its allow rule: %+v", p.Sites)
	}
	if err := run("hil", "approve", held.ID); err != nil || status(held.ID) != "approved" {
		t.Errorf("the operator approves the submit: %v %s", err, status(held.ID))
	}
}
