package publicationnodes

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/publication"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// asTheUsersRun clears what makes a process not the operator's own terminal
// (a `go test` started from an agent's shell inherits its markers).
func asTheUsersRun(t *testing.T) {
	t.Helper()
	for _, k := range append(browserctl.AgentContextEnv(), browserctl.EnvAICaller, workflow.EnvWorkflowRun) {
		t.Setenv(k, "")
	}
}

// sentSend queues a send as an agent and has the user approve it; the send
// runs and is sent. It returns its id.
func sentSend(t *testing.T, ctx context.Context, db *sql.DB) string {
	t.Helper()
	t.Setenv(hilsend.EnvAgentContext, "coder")
	out, handled, err := hilsend.Guard(ctx, "comm.email_send", workflow.NodeInput{}, map[string]interface{}{
		"smtp_host": "smtp.example.test", "from": "me@example.test", "to": "ada@example.test", "subject": "Hello", "body": "Hi"})
	t.Setenv(hilsend.EnvAgentContext, "")
	if err != nil || !handled {
		t.Fatalf("queue a send: handled %v, %v", handled, err)
	}
	id := out[0].Items[0].JSON["hil_id"].(string)
	sent := func(context.Context, hilsend.Run) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	}
	if _, err := hilsend.Resolve(ctx, hilsend.Store{DB: db, Profile: "default"}, id, true, hilsend.Edit{}, false, sent); err != nil {
		t.Fatal(err)
	}
	return id
}

// A run an AI started (the chat's or an agent's `workflow run`, the org side,
// a process that is not the operator's) registers only what belongs to a
// send the user approved, as `publication register` asks of an agent; the
// user's own runs register as before (#831 F2).
func TestRegisterFromAnAIRunNeedsAnApprovedSend(t *testing.T) {
	asTheUsersRun(t)
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	in := workflow.NodeInput{ExecutionID: "execution", NodeID: "register", Items: []workflow.Item{workflow.NewItem(map[string]interface{}{"text": "hello"})}}
	config := func(approval string) map[string]interface{} {
		return map[string]interface{}{"platform": "email", "kind": "post", "body": "{{ $json.text }}", "approval": approval}
	}
	count := func() int {
		entries, err := publication.NewStore(db.DB, "default").List(ctx, publication.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	runs := map[string]func() context.Context{
		"a chat's run": func() context.Context {
			return workflow.WithTrigger(ctx, "trigger.manual", map[string]interface{}{workflow.AICallerKey: "chat"})
		},
		"an org's call":    func() context.Context { return workflow.WithTrigger(ctx, workflow.TriggerTypeOrgTool, nil) },
		"a workflow's run": func() context.Context { t.Setenv(workflow.EnvWorkflowRun, "exec-7"); return ctx },
	}
	for name, run := range runs {
		for _, approval := range []string{"", "missing"} {
			if _, err := (&RegisterNode{}).Execute(run(), in, config(approval)); err == nil || !strings.Contains(err.Error(), "needs an approved send") {
				t.Errorf("%s, approval %q: %v", name, approval, err)
			}
		}
		t.Setenv(workflow.EnvWorkflowRun, "")
	}
	if n := count(); n != 0 {
		t.Fatalf("%d publications registered without approval", n)
	}

	id := sentSend(t, ctx, db.DB)
	other := config(id)
	other["platform"] = "custom"
	if _, err := (&RegisterNode{}).Execute(runs["a chat's run"](), in, other); err == nil || !strings.Contains(err.Error(), "not the publication's") {
		t.Errorf("a publication on another platform than the send's: %v", err)
	}
	if _, err := (&RegisterNode{}).Execute(runs["a chat's run"](), in, config("{{ \""+id+"\" }}")); err != nil {
		t.Errorf("a chat's run with a sent send: %v", err)
	}
	again := workflow.NodeInput{ExecutionID: "execution-2", NodeID: "register", Items: in.Items}
	if _, err := (&RegisterNode{}).Execute(runs["a chat's run"](), again, config(id)); err == nil || !strings.Contains(err.Error(), "already registered a publication") {
		t.Errorf("the approval again: %v", err)
	}
	if _, err := (&RegisterNode{}).Execute(ctx, workflow.NodeInput{ExecutionID: "mine", NodeID: "register", Items: in.Items}, config("")); err != nil {
		t.Errorf("the user's own run: %v", err)
	}
	if n := count(); n != 2 {
		t.Errorf("%d publications, want 2", n)
	}
}

// Each item names its own send: approval is held back from the engine's
// one-pass resolution and evaluated per item.
func TestRegisterApprovalIsPerItem(t *testing.T) {
	asTheUsersRun(t)
	if !slices.Contains((&RegisterNode{}).PerItemConfigFields(), "approval") {
		t.Fatal("approval is resolved once for every item")
	}
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	a, b := sentSend(t, ctx, db.DB), sentSend(t, ctx, db.DB)
	in := workflow.NodeInput{ExecutionID: "execution", NodeID: "register", Items: []workflow.Item{
		workflow.NewItem(map[string]interface{}{"text": "one", "hil_id": a}),
		workflow.NewItem(map[string]interface{}{"text": "two", "hil_id": b}),
	}}
	config := map[string]interface{}{"platform": "email", "kind": "post", "body": "{{ $json.text }}", "approval": "{{ $json.hil_id }}"}
	run := workflow.WithTrigger(ctx, "trigger.manual", map[string]interface{}{workflow.AICallerKey: "chat"})
	if out, err := (&RegisterNode{}).Execute(run, in, config); err != nil || len(out[0].Items) != 2 {
		t.Fatalf("two items, two sends: %v %v", out, err)
	}
}
