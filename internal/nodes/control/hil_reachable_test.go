package control

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/workflow"
)

// subStore is commsStore that also finds the workflows a core.execute_workflow
// node calls.
type subStore struct {
	*commsStore
	workflows map[string]*workflow.Workflow
}

func (s *subStore) GetWorkflow(_ context.Context, id string) (*workflow.Workflow, error) {
	return s.workflows[id], nil
}

func (s *subStore) ListWorkflows(_ context.Context, profile string) ([]workflow.Workflow, error) {
	var out []workflow.Workflow
	for _, wf := range s.workflows {
		if wf.ProfileID == profile {
			out = append(out, *wf)
		}
	}
	return out, nil
}

func node(id, typ string, config map[string]interface{}) workflow.WorkflowNode {
	return workflow.WorkflowNode{ID: id, WorkflowID: "wf1", Type: typ, Name: id, Config: config}
}

func chain(ids ...string) []workflow.WorkflowConnection {
	var out []workflow.WorkflowConnection
	for i := 1; i < len(ids); i++ {
		out = append(out, workflow.WorkflowConnection{SourceNodeID: ids[i-1], SourceHandle: "main", TargetNodeID: ids[i], TargetHandle: "main"})
	}
	return out
}

// runAfterHIL runs trigger → HIL → after (a chain) up to the HIL pause and
// returns whether its item was marked outbound. subs are the workflows the
// store knows.
func runAfterHIL(t *testing.T, after []workflow.WorkflowNode, subs ...*workflow.Workflow) bool {
	t.Helper()
	r := newCommsRig(t, "test.unused", map[string]interface{}{}, nil, map[string]interface{}{"to": "ada@example.test"})
	registered := map[string]bool{}
	for _, n := range after {
		if !registered[n.Type] {
			registered[n.Type] = true
			r.reg.Register(n.Type, func() workflow.NodeExecutor { return fakeSend{typ: n.Type, calls: &r.calls} })
		}
	}
	ids := []string{"t", "hil"}
	r.wf.Nodes = r.wf.Nodes[:2]
	for _, n := range after {
		r.wf.Nodes = append(r.wf.Nodes, n)
		ids = append(ids, n.ID)
	}
	r.wf.Connections = chain(ids...)
	r.wf.ProfileID = "default"
	var err error
	if r.dag, err = workflow.BuildDAG(r.wf.Nodes, r.wf.Connections); err != nil {
		t.Fatal(err)
	}
	store := &subStore{commsStore: r.store, workflows: map[string]*workflow.Workflow{r.wf.ID: r.wf}}
	for _, s := range subs {
		store.workflows[s.ID] = s
	}
	exec := &workflow.WorkflowExecution{ID: "ex1", ProfileID: "default", WorkflowID: "wf1", TriggerNodeID: "t", TriggerData: r.trigger}
	if err := workflow.RunExecution(r.ctx, exec, r.wf, r.dag, r.reg, store, nil, workflow.NewExpressionEngine(), zerolog.Nop()); !errors.Is(err, workflow.ErrExecutionPaused) {
		t.Fatalf("run = %v", err)
	}
	if r.calls != 0 {
		t.Fatal("a node after the HIL node ran before the approval")
	}
	marked, _ := r.nodeConfig()[commsync.OutboundKey].(bool)
	return marked
}

func emailer(id string) *workflow.Workflow {
	return &workflow.Workflow{ID: id, Name: "mailer " + id, ProfileID: "default",
		Nodes: []workflow.WorkflowNode{
			{ID: "st", WorkflowID: id, Type: "trigger.execute_workflow", Name: "Called"},
			{ID: "mail", WorkflowID: id, Type: "comm.email_send", Name: "Mail", Config: map[string]interface{}{"to": "{{ $json.to }}", "body": "Hi"}},
		},
		Connections: []workflow.WorkflowConnection{{SourceNodeID: "st", SourceHandle: "main", TargetNodeID: "mail", TargetHandle: "main"}},
	}
}

// A send anywhere after the HIL node marks its items, not only the first
// node of a branch: HIL → set → email is the user's to approve.
func TestHILMarksASendFurtherDown(t *testing.T) {
	set := map[string]interface{}{"field": "x"}
	if !runAfterHIL(t, []workflow.WorkflowNode{node("set", "transform.set", set), node("mail", "comm.email_send", map[string]interface{}{"to": "{{ $json.to }}", "body": "Hi"})}) {
		t.Error("HIL → transform.set → comm.email_send was not marked")
	}
	if !runAfterHIL(t, []workflow.WorkflowNode{node("if", "core.if", map[string]interface{}{}), node("set", "transform.set", set),
		node("dm", "linkedin.send_dms", map[string]interface{}{"messageText": "Hello"})}) {
		t.Error("HIL → core.if → set → DM was not marked")
	}
	if runAfterHIL(t, []workflow.WorkflowNode{node("set", "transform.set", set), node("set2", "transform.set", set)}) {
		t.Error("HIL → set → set (no send) was marked")
	}
}

// A called workflow is followed: one that emails marks the item, one that
// doesn't leaves it, and one that can't be resolved here, or calls itself
// back, ends the walk; the unresolvable one counts as sending.
func TestHILMarksASendInACalledWorkflow(t *testing.T) {
	call := func(ref string) workflow.WorkflowNode {
		return node("call", "core.execute_workflow", map[string]interface{}{"workflowId": ref})
	}
	if !runAfterHIL(t, []workflow.WorkflowNode{call("sub-mail")}, emailer("sub-mail")) {
		t.Error("HIL → a workflow that emails (by id) was not marked")
	}
	if !runAfterHIL(t, []workflow.WorkflowNode{call("mailer sub-mail")}, emailer("sub-mail")) {
		t.Error("HIL → a workflow that emails (by name) was not marked")
	}
	quiet := &workflow.Workflow{ID: "sub-quiet", Name: "quiet", ProfileID: "default",
		Nodes: []workflow.WorkflowNode{{ID: "st", WorkflowID: "sub-quiet", Type: "trigger.execute_workflow", Name: "Called"},
			{ID: "back", WorkflowID: "sub-quiet", Type: "core.execute_workflow", Name: "Back", Config: map[string]interface{}{"workflowId": "wf1"}}},
		Connections: []workflow.WorkflowConnection{{SourceNodeID: "st", SourceHandle: "main", TargetNodeID: "back", TargetHandle: "main"}},
	}
	if runAfterHIL(t, []workflow.WorkflowNode{call("sub-quiet")}, quiet) {
		t.Error("HIL → a workflow that sends nothing (and calls this one back) was marked")
	}
	for _, ref := range []string{"missing", "{{ $json.which }}"} {
		if !runAfterHIL(t, []workflow.WorkflowNode{call(ref)}, emailer("sub-mail")) {
			t.Errorf("HIL → a workflow that can't be resolved (%q) was not marked", ref)
		}
	}
	other := emailer("sub-other")
	other.ProfileID = "work"
	if !runAfterHIL(t, []workflow.WorkflowNode{call("sub-other")}, other) {
		t.Error("HIL → another profile's workflow was not marked")
	}
}

// An operation only the run resolves counts as sending: a step in front of a
// node that sends for some operations is the user's unless its operation is
// a plain value that doesn't send.
func TestHILMarksATemplatedOperation(t *testing.T) {
	slack := func(op string) workflow.WorkflowNode {
		return node("slack", "comm.slack", map[string]interface{}{"operation": op, "channel": "#general", "text": "Hi"})
	}
	for _, op := range []string{"{{ $json.op }}", "={{$json.op}}", "post_message"} {
		if !runAfterHIL(t, []workflow.WorkflowNode{node("set", "transform.set", map[string]interface{}{}), slack(op)}) {
			t.Errorf("HIL → set → comm.slack operation %q was not marked", op)
		}
	}
	if runAfterHIL(t, []workflow.WorkflowNode{slack("list_channels")}) {
		t.Error("HIL → comm.slack list_channels (no send) was marked")
	}
}
