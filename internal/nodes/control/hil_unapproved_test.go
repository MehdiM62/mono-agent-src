package control

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/workflow"
)

// sendsAfter is the engine's view of a HIL node with an email after it.
func sendsAfter(ctx context.Context) context.Context {
	return commsync.WithReachable(ctx, commsync.Reachable{Nodes: []commsync.Node{
		{ID: "mail", Type: "comm.email_send", Config: map[string]interface{}{"to": "{{ $json.to }}", "body": "Hi"}},
	}})
}

// A workflow the user hasn't approved (one the chat, an agent or a workflow
// wrote) can't settle a step that sends a message with its own auto_decide:
// every item waits for the user, with Jev's suggestion shown.
func TestHILAuto_UnapprovedWorkflowNeverSettlesASend(t *testing.T) {
	db := newAutoTestDB(t)
	jevByCaption(t, nil)
	enable(t, db)
	ctx := workflow.WithAIAuthored(sendsAfter(autoCtx(db)))
	_, err := (&HumanInLoopNode{}).Execute(ctx, threeItems(), autoConfig(map[string]interface{}{"policy": "approve"}))
	if !errors.Is(err, workflow.ErrNodePaused) {
		t.Fatalf("err = %v, want paused for the user", err)
	}
	for _, r := range rowsOf(t, db) {
		c := cfgOf(t, r)
		if r.Status != "pending" || c[commsync.OutboundKey] != true || c["auto_decide_skipped"] != autoDecideHeld ||
			c["suggestion"] == nil || c["decided_by"] != nil || c["p"] != nil {
			t.Fatalf("row = %+v", r)
		}
	}
}

// The user's own workflow keeps its auto_decide in front of a send (the user
// delegating), and an unapproved one keeps it in front of a step that sends
// nothing.
func TestHILAuto_DelegationStillSettles(t *testing.T) {
	for name, ctx := range map[string]func(context.Context) context.Context{
		"the user's workflow, a send after it": sendsAfter,
		"an unapproved workflow, nothing sends after it": func(c context.Context) context.Context {
			return workflow.WithAIAuthored(commsync.WithReachable(c, commsync.Reachable{Nodes: []commsync.Node{{ID: "set", Type: "transform.set"}}}))
		},
		"an unapproved workflow, no engine around": workflow.WithAIAuthored,
	} {
		t.Run(name, func(t *testing.T) {
			db := newAutoTestDB(t)
			jevByCaption(t, nil)
			enable(t, db)
			out, err := (&HumanInLoopNode{}).Execute(ctx(autoCtx(db)), threeItems(), autoConfig(map[string]interface{}{"policy": "approve"}))
			if err != nil {
				t.Fatalf("err = %v, want settled", err)
			}
			if got := captions(out); !reflect.DeepEqual(got, []string{"alpha", "bravo", "charlie"}) {
				t.Fatalf("items = %v", got)
			}
			for _, r := range rowsOf(t, db) {
				if r.Status != "approved" || cfgOf(t, r)["decided_by"] != "jev:jev-test" {
					t.Fatalf("row = %+v", r)
				}
			}
		})
	}
}

// approvalStore is the rig's store, telling the engine whether the user
// approved the workflow's definition.
type approvalStore struct {
	*commsStore
	approved bool
}

func (s *approvalStore) DefinitionApproved(context.Context, string, string) (bool, error) {
	return s.approved, nil
}

// Through the engine: an unapproved workflow's auto_decide in front of an
// email leaves the item for the user and nothing is sent; once the user
// approved the workflow, the same auto_decide settles it and the email runs.
func TestHILAuto_EngineHoldsAnUnapprovedWorkflowsSend(t *testing.T) {
	for _, approved := range []bool{false, true} {
		r := newCommsRig(t, "comm.email_send", map[string]interface{}{"to": "{{ $json.to }}", "subject": "Hi", "body": "Hello"}, nil,
			map[string]interface{}{"to": "ada@example.test", "caption": "hello"})
		jevByCaption(t, nil)
		enable(t, r.db)
		r.wf.Nodes[1].Config = map[string]interface{}{"auto_decide": map[string]interface{}{"policy": "approve"}}
		var err error
		if r.dag, err = workflow.BuildDAG(r.wf.Nodes, r.wf.Connections); err != nil {
			t.Fatal(err)
		}
		exec := &workflow.WorkflowExecution{ID: "ex1", ProfileID: "default", WorkflowID: "wf1", TriggerNodeID: "t", TriggerData: r.trigger}
		err = workflow.RunExecution(r.ctx, exec, r.wf, r.dag, r.reg, &approvalStore{commsStore: r.store, approved: approved}, nil,
			workflow.NewExpressionEngine(), zerolog.Nop())
		var status, nc string
		r.db.QueryRow(`SELECT status, node_config FROM hil_pending WHERE execution_id = 'ex1'`).Scan(&status, &nc)
		switch {
		case !approved && (!errors.Is(err, workflow.ErrExecutionPaused) || status != "pending" || r.calls != 0):
			t.Errorf("unapproved: run %v, item %s (%s), email ran %d times", err, status, nc, r.calls)
		case approved && (err != nil || status != "approved" || r.calls != 1):
			t.Errorf("approved: run %v, item %s (%s), email ran %d times", err, status, nc, r.calls)
		}
	}
}
