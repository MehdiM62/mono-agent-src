package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/workflow"
)

type fixRig struct {
	cfg *globalConfig
}

func newFixRig(t *testing.T, active bool) *fixRig {
	t.Helper()
	cfg := sendEnv(t)
	r := &fixRig{cfg: cfg}
	db, err := initDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := openWorkflowStore(db)
	wf := &workflow.Workflow{ID: "wf-1", Name: "Digest", ProfileID: cfg.ProfileID, IsActive: active, Version: 1,
		Nodes: []workflow.WorkflowNode{
			{ID: "t", WorkflowID: "wf-1", Type: "trigger.manual", Name: "Start", Config: map[string]any{}},
			{ID: "s", WorkflowID: "wf-1", Type: "core.set", Name: "Shape", Config: map[string]any{"assignments": "a=1"}},
			{ID: "h", WorkflowID: "wf-1", Type: "http.request", Name: "Call", Config: map[string]any{"url": "https://example.test/a", "method": "POST"}},
		},
		Connections: []workflow.WorkflowConnection{
			{ID: "c1", WorkflowID: "wf-1", SourceNodeID: "t", TargetNodeID: "s", SourceHandle: "main", TargetHandle: "main"},
			{ID: "c2", WorkflowID: "wf-1", SourceNodeID: "s", TargetNodeID: "h", SourceHandle: "main", TargetHandle: "main"},
		}}
	for i := range wf.Nodes {
		if err := wf.Nodes[i].MarshalConfig(); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveWorkflow(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	// The user saved it themselves: approved.
	if err := store.ApproveDefinition(context.Background(), "wf-1"); err != nil {
		t.Fatal(err)
	}
	if !r.approved(t, "wf-1") {
		t.Fatal("the rig's workflow is not approved to begin with")
	}
	return r
}

func (r *fixRig) approved(t *testing.T, id string) bool {
	t.Helper()
	db, err := initDB(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := openWorkflowStore(db)
	wf, err := store.GetWorkflow(context.Background(), id)
	if err != nil || wf == nil {
		t.Fatalf("workflow %s: %v", id, err)
	}
	ok, err := workflow.IsApproved(context.Background(), store, wf)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func (r *fixRig) workflow(t *testing.T, id string) *workflow.Workflow {
	t.Helper()
	db, err := initDB(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	wf, err := openWorkflowStore(db).GetWorkflow(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return wf
}

// task inserts a task of this profile with the status and result.
func (r *fixRig) task(t *testing.T, kind, status, result string) string {
	t.Helper()
	db, err := initDB(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	task, err := (&aitask.Store{DB: db.DB, Profile: r.cfg.ProfileID}).Insert(aitask.Task{ID: aitask.NewID(), Kind: kind, ConversationID: "c", Status: status, Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		if _, err := db.DB.Exec(`UPDATE ai_tasks SET result_json = ? WHERE id = ?`, result, task.ID); err != nil {
			t.Fatal(err)
		}
	}
	return task.ID
}

func (r *fixRig) run(t *testing.T, args ...string) (map[string]any, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "workflow"}
	cmd.AddCommand(newWorkflowAitaskCmds(r.cfg)...)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	out, err := runSendCLI(t, r.cfg, cmd, args...)
	var body map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &body)
	return body, err
}

func fixResult(kind string, edits ...string) string {
	e := ""
	if len(edits) > 0 {
		e = `,"edits":[` + strings.Join(edits, ",") + `]`
	}
	return `{"summary":"s","diagnosis":"d","workflowId":"wf-1","executionId":"ex-1","fix":{"kind":"` + kind + `","description":"d"` + e + `}}`
}

func editOf(node, config string) string { return `{"node":"` + node + `","config":` + config + `}` }

func codeOf(body map[string]any) string { c, _ := body["code"].(string); return c }

func TestApplyFixAndSaveDraftAreTheOperators(t *testing.T) {
	r := newFixRig(t, false)
	fix := r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("workflow-edit", editOf("s", `{"assignments":"a=2"}`)))
	gen := r.task(t, "workflow-generate", aitask.StatusComplete, `{}`)
	for _, c := range [][]string{{"apply-fix", fix}, {"apply-fix", fix, "--dry-run"}, {"save-draft", gen}} {
		asPlainAgent(t)
		body, err := r.run(t, c...)
		if clicore.ExitCodeFor(err) != 3 || codeOf(body) != "operator_only" {
			t.Errorf("an agent ran %v: %v %v", c, err, body)
		}
	}
	if r.workflow(t, "wf-1").Nodes[1].Config["assignments"] != "a=1" {
		t.Error("a refused apply-fix changed the workflow")
	}
}

func TestApplyFixRefusesTheWrongTask(t *testing.T) {
	r := newFixRig(t, false)
	good := fixResult("workflow-edit", editOf("s", `{"assignments":"a=2"}`))
	for name, c := range map[string]struct{ id, code string }{
		"unknown":    {"at-0000000000000000", "not_found"},
		"wrong kind": {r.task(t, "generic", aitask.StatusComplete, `{"summary":"x"}`), "wrong_kind"},
		"running":    {r.task(t, "failed-run-fix", aitask.StatusRunning, ""), "not_complete"},
		"failed":     {r.task(t, "failed-run-fix", aitask.StatusFailed, ""), "not_complete"},
		"no result":  {r.task(t, "failed-run-fix", aitask.StatusComplete, ""), "no_result"},
		"advice":     {r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("advice")), "no_edit"},
	} {
		body, err := r.run(t, "apply-fix", c.id)
		if err == nil || codeOf(body) != c.code {
			t.Errorf("%s: %v %v, want %s", name, err, body, c.code)
		}
	}
	// A task of another profile is not found, however well-formed.
	id := r.task(t, "failed-run-fix", aitask.StatusComplete, good)
	db, err := initDB(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := (&aitask.Store{DB: db.DB, Profile: "other"}).Insert(aitask.Task{ID: aitask.NewID(), Kind: "failed-run-fix", ConversationID: "c", Status: aitask.StatusComplete, Goal: "g"})
	if err == nil {
		_, err = db.DB.Exec(`UPDATE ai_tasks SET result_json = ? WHERE id = ?`, good, foreign.ID)
	}
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if body, err := r.run(t, "apply-fix", foreign.ID); err == nil || codeOf(body) != "not_found" {
		t.Errorf("another profile's task: %v %v", err, body)
	}
	_ = id
}

func TestApplyFixDryRunThenApplyLeavesTheWorkflowUnapproved(t *testing.T) {
	r := newFixRig(t, false)
	id := r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("workflow-edit", editOf("h", `{"url":"https://example.test/b"}`)))
	body, err := r.run(t, "apply-fix", id, "--dry-run")
	if err != nil || body["ok"] != true || body["workflowId"] != "wf-1" || body["workflowName"] != "Digest" {
		t.Fatalf("%v %v", err, body)
	}
	edits, _ := body["edits"].([]any)
	e, _ := edits[0].(map[string]any)
	if len(edits) != 1 || e["node"] != "h" || e["nodeName"] != "Call" || e["path"] != "url" || e["before"] != "https://example.test/a" || e["after"] != "https://example.test/b" {
		t.Fatalf("edits = %v", edits)
	}
	warns, _ := body["warnings"].([]any)
	if !strings.Contains(toJSON(warns), "new_outbound") {
		t.Errorf("no outbound warning for a POST step: %v", warns)
	}
	if r.workflow(t, "wf-1").Nodes[2].Config["url"] != "https://example.test/a" || !r.approved(t, "wf-1") {
		t.Fatal("--dry-run changed the workflow")
	}

	body, err = r.run(t, "apply-fix", id)
	if err != nil || body["ok"] != true || body["applied"] != float64(1) || body["needsApproval"] != true || body["workflowId"] != "wf-1" {
		t.Fatalf("%v %v", err, body)
	}
	if got := r.workflow(t, "wf-1").Nodes[2].Config["url"]; got != "https://example.test/b" {
		t.Fatalf("url = %v", got)
	}
	// The existing approval step is still required: the edited definition is not the approved one.
	if r.approved(t, "wf-1") {
		t.Error("the workflow is still approved after the edit")
	}
	// The same task again changes nothing: the edit is already in.
	if body, err = r.run(t, "apply-fix", id); err == nil || codeOf(body) != "already_applied" {
		t.Errorf("second apply: %v %v", err, body)
	}
}

func TestApplyFixRefusals(t *testing.T) {
	r := newFixRig(t, false)
	for name, c := range map[string]struct{ result, code string }{
		"invalid merge": {fixResult("workflow-edit", editOf("h", `{"method":"TELEPORT"}`)), "invalid_workflow"},
		"secret key":    {fixResult("workflow-edit", editOf("h", `{"headers":{"Authorization":"x"}}`)), "secret_key"},
		"unknown node":  {fixResult("workflow-edit", editOf("zz", `{"a":1}`)), "unknown_node"},
	} {
		id := r.task(t, "failed-run-fix", aitask.StatusComplete, c.result)
		for _, args := range [][]string{{"apply-fix", id, "--dry-run"}, {"apply-fix", id}} {
			if body, err := r.run(t, args...); err == nil || codeOf(body) != c.code {
				t.Errorf("%s %v: %v %v, want %s", name, args, err, body, c.code)
			}
		}
	}
	if !r.approved(t, "wf-1") || r.workflow(t, "wf-1").Nodes[2].Config["method"] != "POST" {
		t.Error("a refused edit changed the workflow")
	}
}

func TestApplyFixToAnActiveWorkflowNeedsAllowActive(t *testing.T) {
	r := newFixRig(t, true)
	id := r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("workflow-edit", editOf("s", `{"assignments":"a=2"}`)))
	body, err := r.run(t, "apply-fix", id, "--dry-run")
	if err != nil || !strings.Contains(toJSON(body["warnings"]), "workflow_active") {
		t.Fatalf("dry-run: %v %v", err, body)
	}
	if body, err = r.run(t, "apply-fix", id); err == nil || codeOf(body) != "workflow_active_needs_confirm" {
		t.Fatalf("apply: %v %v", err, body)
	}
	if r.workflow(t, "wf-1").Nodes[1].Config["assignments"] != "a=1" {
		t.Fatal("an active workflow was edited without --allow-active")
	}
	if body, err = r.run(t, "apply-fix", id, "--allow-active"); err != nil || body["applied"] != float64(1) {
		t.Fatalf("--allow-active: %v %v", err, body)
	}
	if r.approved(t, "wf-1") {
		t.Error("an edited active workflow is still approved")
	}
}

const draftWorkflow = `{"name":"Fresh","description":"d","is_active":false,
 "nodes":[{"id":"t","type":"trigger.manual","name":"Start","config":{}},{"id":"s","type":"core.set","name":"Shape","config":{"assignments":"a=1"}}],
 "connections":[{"source":"t","target":"s","source_handle":"main","target_handle":"main"}]}`

func TestSaveDraftSavesANewInactiveUnapprovedWorkflow(t *testing.T) {
	r := newFixRig(t, false)
	id := r.task(t, "workflow-generate", aitask.StatusComplete, `{"summary":"s","workflow":`+draftWorkflow+`,"validation":{"valid":true}}`)
	body, err := r.run(t, "save-draft", id)
	if err != nil || body["ok"] != true || body["needsApproval"] != true || body["nodeCount"] != float64(2) || body["name"] != "Fresh" {
		t.Fatalf("%v %v", err, body)
	}
	newID, _ := body["workflowId"].(string)
	if newID == "" || newID == "wf-1" {
		t.Fatalf("workflowId = %q: a draft takes a new id, it never overwrites an existing workflow", newID)
	}
	saved := r.workflow(t, newID)
	if saved == nil || saved.IsActive || len(saved.Nodes) != 2 || saved.ProfileID != r.cfg.ProfileID {
		t.Fatalf("%+v", saved)
	}
	if r.approved(t, newID) {
		t.Error("a saved draft is approved")
	}
	if orig := r.workflow(t, "wf-1"); orig.Name != "Digest" || len(orig.Nodes) != 3 {
		t.Errorf("the workflow with the document's id was overwritten: %+v", orig)
	}
	// The same proposal is saved once.
	body, err = r.run(t, "save-draft", id)
	if err == nil || codeOf(body) != "already_applied" {
		t.Fatalf("second save: %v %v", err, body)
	}
}

// A proposal is applied once, whoever asks again; the audit row says who, which task and workflow, and how many edits, no value.
func TestApplyFixWritesTheAuditLineAndRefusesTheSecondApply(t *testing.T) {
	r := newFixRig(t, false)
	id := r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("workflow-edit", editOf("s", `{"assignments":"a=2"}`)))
	if _, err := r.run(t, "apply-fix", id); err != nil {
		t.Fatal(err)
	}
	db, err := initDB(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var kind, wf, actor, at string
	var edits int
	if err := db.DB.QueryRow(`SELECT kind, workflow_id, edit_count, actor, applied_at FROM ai_task_applies WHERE task_id = ?`, id).Scan(&kind, &wf, &edits, &actor, &at); err != nil {
		t.Fatalf("no audit line: %v", err)
	}
	if kind != "failed-run-fix" || wf != "wf-1" || edits != 1 || actor != "operator" || at == "" {
		t.Fatalf("audit = %s %s %d %s %s", kind, wf, edits, actor, at)
	}
	for _, args := range [][]string{{"apply-fix", id}, {"apply-fix", id, "--dry-run"}, {"apply-fix", id, "--allow-active"}} {
		if body, err := r.run(t, args...); err == nil || codeOf(body) != "already_applied" {
			t.Errorf("%v again: %v %v", args, err, body)
		}
	}
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ai_task_applies WHERE task_id = ?`, id).Scan(&n)
	if n != 1 {
		t.Errorf("%d audit rows", n)
	}
}

// A refused or failed apply leaves no claim, so the user can ask again after fixing the cause.
func TestARefusedApplyLeavesNoClaim(t *testing.T) {
	r := newFixRig(t, true)
	id := r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("workflow-edit", editOf("s", `{"assignments":"a=2"}`)))
	if _, err := r.run(t, "apply-fix", id); err == nil {
		t.Fatal("an active workflow was edited without --allow-active")
	}
	db, _ := initDB(r.cfg)
	defer db.Close()
	var n int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM ai_task_applies`).Scan(&n)
	if n != 0 {
		t.Fatalf("a refused apply left %d claim(s)", n)
	}
	if _, err := r.run(t, "apply-fix", id, "--allow-active"); err != nil {
		t.Fatalf("after the refusal: %v", err)
	}
}

// The workflow changed after the task completed: the person looked at another workflow.
func TestApplyFixRefusesAStaleProposal(t *testing.T) {
	r := newFixRig(t, false)
	id := r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("workflow-edit", editOf("s", `{"assignments":"a=2"}`)))
	// The user edits a node (workflow node set) after the task completed.
	db, err := initDB(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	store := openWorkflowStore(db)
	wf, _ := store.GetWorkflow(context.Background(), "wf-1")
	wf.Nodes[2].Config["url"] = "https://example.test/changed"
	if err := store.SaveWorkflowNodes(context.Background(), "wf-1", wf.Nodes); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for _, args := range [][]string{{"apply-fix", id, "--dry-run"}, {"apply-fix", id}} {
		if body, err := r.run(t, args...); err == nil || codeOf(body) != "stale_proposal" {
			t.Errorf("%v: %v %v", args, err, body)
		}
	}
	if r.workflow(t, "wf-1").Nodes[1].Config["assignments"] != "a=1" {
		t.Fatal("a stale proposal was applied")
	}
}

// The argv the app sends: global flags before the verb, the task id after `--`.
func TestApplyFixAndSaveDraftTakeTheAppsArgv(t *testing.T) {
	r := newFixRig(t, false)
	fix := r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("workflow-edit", editOf("s", `{"assignments":"a=2"}`)))
	gen := r.task(t, "workflow-generate", aitask.StatusComplete, `{"summary":"s","workflow":`+draftWorkflow+`,"validation":{"valid":true}}`)
	root := func(args ...string) (map[string]any, error) {
		cmd := newRootCmd()
		cmd.SetArgs(append([]string{"--profile", r.cfg.ProfileID, "--db-path", r.cfg.DBPath, "--json"}, args...))
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		var err error
		out := captureStdout(t, func() { err = cmd.Execute() })
		var body map[string]any
		_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &body)
		return body, err
	}
	if body, err := root("workflow", "apply-fix", "--dry-run", "--", fix); err != nil || body["ok"] != true || body["workflowId"] != "wf-1" {
		t.Fatalf("dry-run: %v %v", err, body)
	}
	if body, err := root("workflow", "apply-fix", "--", fix); err != nil || body["applied"] != float64(1) {
		t.Fatalf("apply: %v %v", err, body)
	}
	if body, err := root("workflow", "save-draft", "--", gen); err != nil || body["ok"] != true || body["needsApproval"] != true {
		t.Fatalf("save-draft: %v %v", err, body)
	}
	// A task id is a plain id, never a path or a flag.
	for _, bad := range []string{"../../etc/passwd", "/tmp/x.json", "a b", "-rf", ""} {
		if body, err := root("workflow", "apply-fix", "--", bad); err == nil || (codeOf(body) != "invalid_input" && body != nil) {
			t.Errorf("%q: %v %v", bad, err, body)
		}
	}
}

// A task's own confined session is an agent: it can propose, never apply.
func TestATaskSessionCannotApplyItsOwnProposal(t *testing.T) {
	r := newFixRig(t, false)
	id := r.task(t, "failed-run-fix", aitask.StatusComplete, fixResult("workflow-edit", editOf("s", `{"assignments":"a=2"}`)))
	asTaskSession(t, id, "tok")
	if body, err := r.run(t, "apply-fix", id, "--allow-active"); err == nil || codeOf(body) != "operator_only" {
		t.Fatalf("%v %v", err, body)
	}
	if r.workflow(t, "wf-1").Nodes[1].Config["assignments"] != "a=1" {
		t.Fatal("a task session applied its own proposal")
	}
}

func TestSaveDraftRefusals(t *testing.T) {
	r := newFixRig(t, false)
	for name, c := range map[string]struct{ id, code string }{
		"wrong kind": {r.task(t, "failed-run-fix", aitask.StatusComplete, `{}`), "wrong_kind"},
		"running":    {r.task(t, "workflow-generate", aitask.StatusRunning, ""), "not_complete"},
		"no result":  {r.task(t, "workflow-generate", aitask.StatusComplete, ""), "no_result"},
		"unknown":    {"at-0000000000000000", "not_found"},
		// A result that no longer passes the validator is not saved, whatever its validation block says.
		"invalid workflow": {r.task(t, "workflow-generate", aitask.StatusComplete,
			`{"summary":"s","workflow":{"name":"n","nodes":[{"id":"h","type":"http.request","name":"H","config":{}}]},"validation":{"valid":true}}`), "bad_result"},
		"active workflow": {r.task(t, "workflow-generate", aitask.StatusComplete,
			`{"summary":"s","workflow":{"name":"n","is_active":true,"nodes":[{"id":"t","type":"trigger.manual","name":"T","config":{}}]},"validation":{"valid":true}}`), "bad_result"},
	} {
		if body, err := r.run(t, "save-draft", c.id); err == nil || codeOf(body) != c.code {
			t.Errorf("%s: %v %v, want %s", name, err, body, c.code)
		}
	}
	db, err := initDB(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	list, _ := openWorkflowStore(db).ListWorkflows(context.Background(), r.cfg.ProfileID)
	if len(list) != 1 {
		t.Errorf("%d workflows after refused saves, want the 1 the rig made", len(list))
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
