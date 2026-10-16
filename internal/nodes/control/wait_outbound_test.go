package control

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

func withSendClassifier(t *testing.T) {
	t.Helper()
	workflow.SetSendClassifier(hilsend.MaySend)
	t.Cleanup(func() { workflow.SetSendClassifier(nil) })
}

// runPaused runs trigger → first → after up to its pause, as a run of
// triggerType ("" = a manual run).
func runPaused(t *testing.T, first workflow.WorkflowNode, after []workflow.WorkflowNode, triggerType string) *commsRig {
	t.Helper()
	r := newCommsRig(t, "test.unused", map[string]interface{}{}, nil, map[string]interface{}{"to": "ada@example.test"})
	if first.Type == "core.wait" {
		r.reg.Register("core.wait", func() workflow.NodeExecutor { return &WaitNode{} })
	}
	registered := map[string]bool{}
	for _, n := range after {
		if !registered[n.Type] {
			registered[n.Type] = true
			r.reg.Register(n.Type, func() workflow.NodeExecutor { return fakeSend{typ: n.Type, calls: &r.calls} })
		}
	}
	r.wf.Nodes = append([]workflow.WorkflowNode{r.wf.Nodes[0], first}, after...)
	ids := []string{"t"}
	for _, n := range r.wf.Nodes[1:] {
		ids = append(ids, n.ID)
	}
	r.wf.Connections = chain(ids...)
	r.wf.ProfileID = "default"
	var err error
	if r.dag, err = workflow.BuildDAG(r.wf.Nodes, r.wf.Connections); err != nil {
		t.Fatal(err)
	}
	exec := &workflow.WorkflowExecution{ID: "ex1", ProfileID: "default", WorkflowID: "wf1", TriggerNodeID: "t", TriggerType: triggerType, TriggerData: r.trigger}
	store := &subStore{commsStore: r.store, workflows: map[string]*workflow.Workflow{r.wf.ID: r.wf}}
	if err := workflow.RunExecution(r.ctx, exec, r.wf, r.dag, r.reg, store, nil, workflow.NewExpressionEngine(), zerolog.Nop()); !errors.Is(err, workflow.ErrExecutionPaused) {
		t.Fatalf("run = %v", err)
	}
	if r.calls != 0 {
		t.Fatal("a node after the pause ran")
	}
	return r
}

func replyMarked(t *testing.T, r *commsRig) bool {
	t.Helper()
	var spec string
	if err := r.db.QueryRow(`SELECT spec FROM wait_replies WHERE execution_id = 'ex1'`).Scan(&spec); err != nil {
		t.Fatal(err)
	}
	var s workflow.WaitReplySpec
	if err := json.Unmarshal([]byte(spec), &s); err != nil {
		t.Fatal(err)
	}
	return s.Outbound
}

// A wait answered through a reply link (core.wait's resume URL here) in
// front of a send is marked when it opens; in front of nothing that sends
// it is not.
func TestWaitReplyInFrontOfASendIsMarked(t *testing.T) {
	withSendClassifier(t)
	wait := node("wait", "core.wait", map[string]interface{}{"resume": "webhook"})
	set := map[string]interface{}{"field": "x"}
	mail := node("mail", "comm.email_send", map[string]interface{}{"to": "{{ $json.to }}", "body": "Hi"})
	if !replyMarked(t, runPaused(t, wait, []workflow.WorkflowNode{node("set", "transform.set", set), mail}, "")) {
		t.Error("core.wait → set → email: the reply was not marked")
	}
	if replyMarked(t, runPaused(t, wait, []workflow.WorkflowNode{node("set", "transform.set", set), node("set2", "transform.set", set)}, "")) {
		t.Error("core.wait → set → set: the reply was marked")
	}
}

// A run another workflow called hands its output back to the caller, whose
// later nodes it can't see: its Human-in-Loop items count as sending.
func TestHILInACalledRunIsMarked(t *testing.T) {
	r := runPaused(t, node("hil", "core.human_in_loop", map[string]interface{}{}),
		[]workflow.WorkflowNode{node("set", "transform.set", map[string]interface{}{})}, workflow.TriggerTypeExecuteWorkflow)
	if marked, _ := r.nodeConfig()[commsync.OutboundKey].(bool); !marked {
		t.Error("a Human-in-Loop item of a called run was not marked")
	}
}
