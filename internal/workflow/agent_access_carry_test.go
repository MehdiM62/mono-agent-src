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
