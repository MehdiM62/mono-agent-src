package aitasknodes

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

type queued struct {
	calls int
	in    aitask.SubmitInput
}

// asOperator clears the markers an agent's shell carries, so the process
// reads as the user's own terminal whoever runs the tests.
func asOperator(t *testing.T) {
	t.Helper()
	for _, k := range append(browserctl.AgentContextEnv(), workflow.EnvWorkflowRun, browserctl.EnvAICaller,
		browserctl.EnvAgentKind, browserctl.EnvAgentKindToken) {
		t.Setenv(k, "")
	}
}

// rig installs a fake Queue and returns what it saw.
func rig(t *testing.T) *queued {
	t.Helper()
	asOperator(t)
	q := &queued{}
	oldQ, oldDB := Queue, globalDB
	Queue = func(_ context.Context, _ *sql.DB, profile string, in aitask.SubmitInput) (aitask.Task, error) {
		q.calls++
		q.in = in
		return aitask.Task{ID: "at-1", ConversationID: "conv-1", Status: aitask.StatusQueued}, nil
	}
	globalDB = new(sql.DB)
	t.Cleanup(func() { Queue, globalDB = oldQ, oldDB })
	return q
}

func runCtx() context.Context { return vault.ContextWithProfileID(context.Background(), "p1") }

func exec(ctx context.Context, cfg map[string]interface{}) ([]workflow.NodeOutput, error) {
	return (&TaskNode{}).Execute(ctx, workflow.NodeInput{WorkflowID: "wf-1", ExecutionID: "e-1"}, cfg)
}

func TestNodeQueuesAndReturnsAtOnce(t *testing.T) {
	q := rig(t)
	out, err := exec(runCtx(), map[string]interface{}{
		"goal": "tidy", "kind": "generic", "context": []interface{}{"selection=/tmp/x"},
		// None of these can start the task or run it unattended.
		"queue_only": false, "unattended": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].Items) != 1 {
		t.Fatalf("output = %+v", out)
	}
	got := out[0].Items[0].JSON
	if got["taskId"] != "at-1" || got["conversationId"] != "conv-1" || got["status"] != "queued" {
		t.Fatalf("item = %v", got)
	}
	if !q.in.QueueOnly || q.in.Unattended {
		t.Fatalf("the node asked to start the task: %+v", q.in)
	}
	if len(q.in.Context) != 1 || q.in.Context[0].Class != "selection" {
		t.Fatalf("context = %+v", q.in.Context)
	}
}

// An AI-authored workflow, and any run an AI started, open no task. Without
// the refusal these runs queue one, which is what the test catches.
func TestNodeRefusesARunThatIsNotTheOperators(t *testing.T) {
	cases := map[string]context.Context{
		"an unapproved workflow": workflow.WithAIAuthored(runCtx()),
		"a run an agent started": workflow.WithTrigger(runCtx(), "trigger.manual", map[string]interface{}{workflow.AICallerKey: "other/org"}),
		"the chat's run":         workflow.WithTrigger(runCtx(), "trigger.manual", map[string]interface{}{workflow.AICallerKey: "chat"}),
		"an org trigger":         workflow.WithTrigger(runCtx(), workflow.TriggerNodeTypeOrg, nil),
	}
	for name, ctx := range cases {
		q := rig(t)
		if _, err := exec(ctx, map[string]interface{}{"goal": "tidy"}); err == nil {
			t.Errorf("%s: queued a task", name)
		}
		if q.calls != 0 {
			t.Errorf("%s: the queue was called %d times", name, q.calls)
		}
	}
	q := rig(t)
	_, err := exec(workflow.WithAIAuthored(runCtx()), map[string]interface{}{"goal": "tidy"})
	if !errors.Is(err, workflow.ErrUnapprovedProgram) || !strings.Contains(err.Error(), "workflow activate wf-1") {
		t.Fatalf("err = %v, want the unapproved-workflow refusal naming wf-1", err)
	}
	if q.calls != 0 {
		t.Fatal("queued")
	}
}

func TestNodeRefusesAMarkedProcess(t *testing.T) {
	q := rig(t)
	t.Setenv(workflow.EnvWorkflowRun, "exec-9")
	if _, err := exec(runCtx(), map[string]interface{}{"goal": "tidy"}); err == nil || q.calls != 0 {
		t.Fatalf("a workflow-marked process queued a task: %v, %d calls", err, q.calls)
	}
}

// Messages and people never reach a task, whatever the workflow says.
func TestNodeRefusesContextFromMessages(t *testing.T) {
	q := rig(t)
	for _, entry := range []string{"messages=/tmp/inbox", "person=/tmp/p", "/tmp/bare", "="} {
		if _, err := exec(runCtx(), map[string]interface{}{"goal": "g", "context": []interface{}{entry}}); err == nil {
			t.Errorf("%q was accepted", entry)
		}
	}
	if q.calls != 0 {
		t.Fatal("queued")
	}
	if _, err := exec(runCtx(), map[string]interface{}{"context": []interface{}{"facts=/tmp/f"}}); err == nil {
		t.Fatal("a task with no goal was queued")
	}
}

func TestNodeWithoutAQueueSaysSo(t *testing.T) {
	q := rig(t)
	Queue = nil
	if _, err := exec(runCtx(), map[string]interface{}{"goal": "g"}); err == nil || q.calls != 0 {
		t.Fatalf("err = %v", err)
	}
}

func TestNodeRequiresTheRunsProfile(t *testing.T) {
	rig(t)
	if _, err := exec(context.Background(), map[string]interface{}{"goal": "g"}); err == nil {
		t.Fatal("ran with no profile")
	}
	if _, err := exec(runCtx(), map[string]interface{}{"goal": "g", "profile_id": "other"}); err == nil {
		t.Fatal("wrote into another profile")
	}
}
