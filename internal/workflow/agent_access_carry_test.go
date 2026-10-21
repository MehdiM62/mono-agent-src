package workflow

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/vault"
)

// Review of #944 (A2): a sub-workflow called by a run under agent access
// keeps masking the values the parent read.
func TestAgentAccessMaskingCarriesIntoASubWorkflow(t *testing.T) {
	e := newAccessEnv(t)
	e.grant(t, secrets.AccessGrant{AnyHost: true})
	eng := newTestEngine(t, e.store, e.reg)
	child := chain("wf-child", step("c1", "test.renameprobe", "Rename", nil))
	child.Nodes[0].Type = TriggerTypeExecuteWorkflow
	createSQLWorkflow(t, e.store, child)

	parent := secrets.AllowResolving(e.ctx, e.db.DB, "default", secrets.AccessOrigin{WorkflowID: "wf-parent", ExecutionID: "p1"})
	if _, err := secrets.Resolve(parent, e.db.DB, "default", "@secret:vs"); err != nil {
		t.Fatal(err)
	}
	parent = WithTrigger(vault.ContextWithExecIDs(parent, "wf-parent", "p1"), "trigger.schedule", map[string]interface{}{})
	id, err := eng.StartSubWorkflow(parent, SubWorkflowStart{ParentExecutionID: "p1", ParentNodeID: "call", Workflow: "wf-child", ItemIndex: -1,
		Items: []Item{NewItem(map[string]interface{}{"v": valVS})}})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := e.store.GetExecution(e.ctx, id)
	if err != nil || rec == nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(rec.TriggerData); strings.Contains(string(b), valVS) {
		t.Errorf("the child's stored trigger data holds the value: %s", b)
	}
	names, _ := rec.TriggerData[SecretNamesKey].([]interface{})
	if len(names) != 1 || names[0] != "vs" {
		t.Fatalf("the child does not know which secrets its parent read: %v", rec.TriggerData[SecretNamesKey])
	}

	// The queue hands the child the real items; its run masks them.
	cur, _ := e.store.GetWorkflow(e.ctx, "wf-child")
	dag, _ := BuildDAG(cur.Nodes, cur.Connections)
	exec := &WorkflowExecution{ID: uuid.NewString(), WorkflowID: "wf-child", ProfileID: "default", TriggerType: TriggerTypeExecuteWorkflow, TriggerNodeID: "wf-child-t",
		TriggerData: map[string]interface{}{
			SubWorkflowItemsKey: []interface{}{map[string]interface{}{"v": valVS}},
			SecretNamesKey:      []interface{}{"vs"},
			UnapprovedCallerKey: "wf-parent",
		}}
	if err := e.store.CreateExecution(e.ctx, exec); err != nil {
		t.Fatal(err)
	}
	if err := RunExecution(e.ctx, exec, cur, dag, e.reg, e.store, nil, NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if got := e.stored(t, exec.ID); strings.Contains(got, valVS) {
		t.Errorf("the child's record holds the parent's secret: %s", got)
	}
}

// Review of #944 (A4): a run that pauses and resumes keeps masking.
func TestAgentAccessMaskingSurvivesPauseAndResume(t *testing.T) {
	e := newAccessEnv(t)
	e.grant(t, secrets.AccessGrant{AnyHost: true})
	open := false
	e.reg.Register("test.gate", func() NodeExecutor { return gateNode{&open} })
	wf := chain("wf-resume",
		step("g1", "test.echoprobe", "Echo", map[string]interface{}{"v": "@secret:vs"}),
		step("g2", "test.gate", "Gate", nil),
		step("g3", "test.renameprobe", "Rename", nil),
	)
	createSQLWorkflow(t, e.store, wf)
	cur, _ := e.store.GetWorkflow(e.ctx, wf.ID)
	dag, _ := BuildDAG(cur.Nodes, cur.Connections)
	exec := &WorkflowExecution{ID: uuid.NewString(), WorkflowID: wf.ID, ProfileID: "default", TriggerType: "trigger.schedule", TriggerNodeID: wf.ID + "-t"}
	if err := e.store.CreateExecution(e.ctx, exec); err != nil {
		t.Fatal(err)
	}
	err := RunExecution(e.ctx, exec, cur, dag, e.reg, e.store, nil, NewExpressionEngine(), zerolog.Nop())
	if !errors.Is(err, ErrExecutionPaused) {
		t.Fatalf("first run: %v", err)
	}
	waiting, _ := e.store.GetExecution(e.ctx, exec.ID)
	if waiting == nil || waiting.ResumeState == "" {
		t.Fatal("no resume state")
	}
	open = true
	exec2 := &WorkflowExecution{ID: exec.ID, WorkflowID: wf.ID, ProfileID: "default", TriggerType: "trigger.schedule", TriggerNodeID: wf.ID + "-t", ResumeState: waiting.ResumeState}
	if err := RunExecution(e.ctx, exec2, cur, dag, e.reg, e.store, nil, NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := e.stored(t, exec.ID); strings.Contains(got, valVS) {
		t.Errorf("a resumed run stored the value: %s", got)
	}
}

// Review of #944 (A3): the forms a step commonly derives from a value are
// masked too (best effort; an expression or code step can still defeat it).
func TestAgentAccessMasksCommonDerivedForms(t *testing.T) {
	e := newAccessEnv(t)
	e.grant(t, secrets.AccessGrant{AnyHost: true})
	ctx := secrets.AllowResolving(e.ctx, e.db.DB, "default", secrets.AccessOrigin{WorkflowID: "w", ExecutionID: "x"})
	const v = `Ab-cd_ef"gh/ij+kl`
	if _, err := secrets.Add(e.ctx, e.db.DB, "default", "secret", "odd", map[string]string{"secret": v}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.Resolve(ctx, e.db.DB, "default", "@secret:odd"); err != nil {
		t.Fatal(err)
	}
	enc, _ := json.Marshal(v)
	for name, form := range map[string]string{
		"std base64":   base64.StdEncoding.EncodeToString([]byte(v)),
		"url base64":   base64.URLEncoding.EncodeToString([]byte(v)),
		"raw base64":   base64.RawURLEncoding.EncodeToString([]byte(v)),
		"url-encoded":  url.QueryEscape(v),
		"path-encoded": url.PathEscape(v),
		"json":         string(enc[1 : len(enc)-1]),
		"upper":        strings.ToUpper(v),
		"lower":        strings.ToLower(v),
		"swapped":      strings.NewReplacer("-", "_", "_", "-").Replace(v),
		"hex":          hex.EncodeToString([]byte(v)),
	} {
		if got := secrets.MaskResolved(ctx, "x "+form+" y"); strings.Contains(got, form) {
			t.Errorf("%s form is not masked: %s", name, got)
		}
	}
}

var _ = context.Background

// Review of #944 (A5): under a host-bound grant only the nodes that check
// their destination read secrets.
func TestAgentAccessHostBoundGrantRefusesNodesWithoutADestinationCheck(t *testing.T) {
	e := newAccessEnv(t)
	e.reg.Register("http.request", func() NodeExecutor { return echoProbe{} })
	e.grant(t, secrets.AccessGrant{Hosts: []string{"api.example.com"}})
	cfg := map[string]interface{}{"v": "@secret:vs"}
	execID, err := e.run(t, chain("wf-mail", step("n1", "test.echoprobe", "Mail-like", cfg)), nil)
	if !errors.Is(err, secrets.ErrResolveRefused) || !strings.Contains(err.Error(), "destination check") {
		t.Fatalf("a node with no destination check read a secret: %v", err)
	}
	if strings.Contains(e.stored(t, execID), valVS) {
		t.Error("the value was read")
	}
	if _, err := e.run(t, chain("wf-http", step("n2", "http.request", "Http", cfg)), nil); err != nil {
		t.Errorf("a node with a destination check: %v", err)
	}
	e.grant(t, secrets.AccessGrant{AnyHost: true})
	if _, err := e.run(t, chain("wf-any", step("n3", "test.echoprobe", "Any", cfg)), nil); err != nil {
		t.Errorf("--any-host: %v", err)
	}
}

// Review of #944 (N1): a step that resolved a secret is held to the grant's
// hosts whatever form the secret took on its way out.
func TestAgentAccessStepThatResolvedASecretCarriesIt(t *testing.T) {
	e := newAccessEnv(t)
	e.grant(t, secrets.AccessGrant{Hosts: []string{"api.example.com"}})
	ctx := secrets.AllowResolving(e.ctx, e.db.DB, "default", secrets.AccessOrigin{WorkflowID: "w", ExecutionID: "x"})
	reader := secrets.WithAccessNode(ctx, "n1", "Reader")
	if _, err := secrets.Resolve(reader, e.db.DB, "default", "@secret:vs"); err != nil {
		t.Fatal(err)
	}
	// Nothing in the texts holds the value (a form nobody listed).
	if err := secrets.CheckSendHost(reader, "http://127.0.0.1/x", "dXNlcjpzb21ldGhpbmc="); !errors.Is(err, secrets.ErrHostNotAllowed) {
		t.Errorf("the step that read a secret sent to a foreign host: %v", err)
	}
	if err := secrets.CheckSendHost(reader, "http://api.example.com/x", "anything"); err != nil {
		t.Errorf("a granted host: %v", err)
	}
	other := secrets.WithAccessNode(ctx, "n2", "Other")
	if err := secrets.CheckSendHost(other, "http://127.0.0.1/x", "anything"); err != nil {
		t.Errorf("a step that read no secret was held: %v", err)
	}
}

// Review of #944 (N5): the case variants of a plain word are not tracked.
func TestAgentAccessCaseVariantsOfAPlainWordAreNotMasked(t *testing.T) {
	e := newAccessEnv(t)
	e.grant(t, secrets.AccessGrant{AnyHost: true})
	if _, err := secrets.Add(e.ctx, e.db.DB, "default", "secret", "word", map[string]string{"secret": "Status"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	ctx := secrets.AllowResolving(e.ctx, e.db.DB, "default", secrets.AccessOrigin{WorkflowID: "w", ExecutionID: "x"})
	if _, err := secrets.Resolve(ctx, e.db.DB, "default", "@secret:word"); err != nil {
		t.Fatal(err)
	}
	if got := secrets.MaskResolved(ctx, "status STATUS Status"); got != "status STATUS ***" {
		t.Errorf("got %q", got)
	}
}

// Review of #944 (N4): a secret the parent passed that cannot be read again
// fails the child instead of leaving the parent's value unmasked.
func TestAgentAccessChildFailsWhenAPassedSecretCannotBeReread(t *testing.T) {
	e := newAccessEnv(t)
	e.grant(t, secrets.AccessGrant{AnyHost: true, Secrets: []string{"other"}})
	child := chain("wf-child4", step("c1", "test.renameprobe", "Rename", nil))
	child.Nodes[0].Type = TriggerTypeExecuteWorkflow
	createSQLWorkflow(t, e.store, child)
	cur, _ := e.store.GetWorkflow(e.ctx, "wf-child4")
	dag, _ := BuildDAG(cur.Nodes, cur.Connections)
	exec := &WorkflowExecution{ID: uuid.NewString(), WorkflowID: "wf-child4", ProfileID: "default", TriggerType: TriggerTypeExecuteWorkflow, TriggerNodeID: "wf-child4-t",
		TriggerData: map[string]interface{}{
			SubWorkflowItemsKey: []interface{}{map[string]interface{}{"v": valVS}},
			SecretNamesKey:      []interface{}{"vs"},
			UnapprovedCallerKey: "wf-parent",
		}}
	if err := e.store.CreateExecution(e.ctx, exec); err != nil {
		t.Fatal(err)
	}
	err := RunExecution(e.ctx, exec, cur, dag, e.reg, e.store, nil, NewExpressionEngine(), zerolog.Nop())
	if err == nil || !strings.Contains(err.Error(), "can no longer be masked") {
		t.Fatalf("the child ran with a secret it could not mask: %v", err)
	}
	if got := e.stored(t, exec.ID); strings.Contains(got, valVS) {
		t.Errorf("the record holds the value: %s", got)
	}
}

// Review of #944 (N3): a queued child runs on the real trigger data the
// queue holds, not on the masked stored copy.
func TestAgentAccessQueuedChildRunsOnTheRealTriggerData(t *testing.T) {
	e := newAccessEnv(t)
	e.grant(t, secrets.AccessGrant{AnyHost: true})
	var seen []string
	e.reg.Register("test.seeprobe", func() NodeExecutor {
		return nodeFunc{typ: "test.seeprobe", fn: func(_ context.Context, in NodeInput) error {
			for _, it := range in.Items {
				if v, ok := it.JSON["v"].(string); ok {
					seen = append(seen, v)
				}
			}
			return nil
		}}
	})
	eng := newTestEngine(t, e.store, e.reg)
	child := chain("wf-child3", step("c1", "test.seeprobe", "See", nil))
	child.Nodes[0].Type = TriggerTypeExecuteWorkflow
	createSQLWorkflow(t, e.store, child)

	parent := secrets.AllowResolving(e.ctx, e.db.DB, "default", secrets.AccessOrigin{WorkflowID: "wf-parent", ExecutionID: "p1"})
	if _, err := secrets.Resolve(parent, e.db.DB, "default", "@secret:vs"); err != nil {
		t.Fatal(err)
	}
	parent = WithTrigger(vault.ContextWithExecIDs(parent, "wf-parent", "p1"), "trigger.schedule", map[string]interface{}{})
	id, err := eng.StartSubWorkflow(parent, SubWorkflowStart{ParentExecutionID: "p1", ParentNodeID: "call", Workflow: "wf-child3", ItemIndex: -1,
		Items: []Item{NewItem(map[string]interface{}{"v": valVS})}})
	if err != nil {
		t.Fatal(err)
	}
	eng.handleExecution(e.ctx, ExecutionRequest{WorkflowID: "wf-child3", ExecutionID: id, TriggerType: TriggerTypeExecuteWorkflow, TriggerNodeID: "wf-child3-t",
		TriggerData: map[string]interface{}{
			SubWorkflowItemsKey: []interface{}{map[string]interface{}{"v": valVS}},
			SecretNamesKey:      []string{"vs"},
			UnapprovedCallerKey: "wf-parent",
		}})
	if len(seen) != 1 || seen[0] != valVS {
		t.Errorf("the child saw %v, want the real value", seen)
	}
	if got := e.stored(t, id); strings.Contains(got, valVS) {
		t.Errorf("the child's record holds the value: %s", got)
	}
}
