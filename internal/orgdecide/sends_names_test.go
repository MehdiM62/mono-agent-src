package orgdecide

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A grant's workflow sends when its send is saved under a legacy name or
// with an operation that is no string, as `org approve` and a decider read
// it (ItemSends): the item is the user's. These read as no send before, so a
// decider could approve the call.
func TestGrantSendsUnderEveryNameAndOperation(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	one := func(n workflow.WorkflowNode) *workflow.Workflow {
		return &workflow.Workflow{ID: "wf-" + n.ID, ProfileID: "p", Nodes: []workflow.WorkflowNode{n}}
	}
	workflows := map[string]*workflow.Workflow{}
	want := map[string]bool{}
	for _, c := range []struct {
		node  workflow.WorkflowNode
		sends bool
	}{
		{workflow.WorkflowNode{ID: "telegram", Type: "telegram", Config: map[string]interface{}{"chat_id": "42", "message": "Hi"}}, true},
		{workflow.WorkflowNode{ID: "gmail", Type: "gmail", Config: map[string]interface{}{"operation": "send_message"}}, true},
		{workflow.WorkflowNode{ID: "mastodon", Type: "comm.mastodon", Config: map[string]interface{}{"operation": "create_status"}}, true},
		{workflow.WorkflowNode{ID: "discord", Type: "service.discord", Config: map[string]interface{}{"operation": float64(5), "content": "Hi"}}, true},
		{workflow.WorkflowNode{ID: "outlook", Type: "comm.outlook_send", Config: map[string]interface{}{"to": "a@b.test", "body": "Hi"}}, true},
		{workflow.WorkflowNode{ID: "read", Type: "comm.outlook_read", Config: map[string]interface{}{"mailbox": "INBOX"}}, false},
		{workflow.WorkflowNode{ID: "set", Type: "core.set"}, false},
	} {
		wf := one(c.node)
		workflows[wf.ID] = wf
		want[c.node.ID] = c.sends
		if _, err := orggrant.NewStore(db).UpsertGrant(ctx, orggrant.GrantInput{ProfileID: "p", OrgName: "growth", RoleID: "writer",
			Tool: orggrant.Tool{Alias: c.node.ID, WorkflowID: wf.ID, Tier: orgdesign.TierConsequential}}); err != nil {
			t.Fatal(err)
		}
	}
	reads := workflowReads{load: func(_ context.Context, id string) (*workflow.Workflow, error) { return workflows[id], nil }}
	for alias, sends := range want {
		if got := ItemSends(ctx, db, "p", "growth", Item{Kind: KindApproval, Class: "grant:" + alias}, reads); got != sends {
			t.Errorf("grant:%s sends = %v, want %v", alias, got, sends)
		}
	}
}
