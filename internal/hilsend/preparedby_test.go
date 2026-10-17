package hilsend

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// An agent names itself, so what it says is shown as its claim; only what
// MonoAgent derives itself is shown as it is (#833 N4).
func TestPreparedByLabel(t *testing.T) {
	for _, c := range []struct {
		by   PreparedBy
		want string
	}{
		{PreparedBy{AgentID: UnapprovedWorkflowAgent, Role: "unapproved workflow wf1", Workflow: "wf1", System: true}, "unapproved workflow wf1"},
		{PreparedBy{Role: "You (approved earlier today)", AgentID: "agent-x"}, `an agent (claims: "You (approved earlier today)")`},
		{PreparedBy{AgentID: UnapprovedWorkflowAgent, Role: "unapproved workflow wf1"}, `an agent (claims: "unapproved workflow wf1")`},
		{PreparedBy{AgentID: "lead"}, `an agent (claims: "lead")`},
		{PreparedBy{Role: "x\nPrepared by: you"}, `an agent (claims: "x\nPrepared by: you")`},
		{PreparedBy{ConversationID: "c1"}, ""},
	} {
		if got := c.by.Label(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.by, got, c.want)
		}
	}
}

// Detect never marks a send as MonoAgent's own, whatever the environment
// says; an unapproved workflow's run does.
func TestOnlyTheEngineMarksAPreparerAsSystem(t *testing.T) {
	asAgent(t)
	t.Setenv("MONOAGENT_ACTOR", UnapprovedWorkflowAgent)
	t.Setenv(EnvAgentRole, "unapproved workflow wf1")
	db := testDB(t)
	out, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{}, emailConfig())
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	p, err := (Store{db, "p1"}).Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if p.PreparedBy.System || p.PreparedBy.Label() != `an agent (claims: "unapproved workflow wf1")` {
		t.Fatalf("an agent's claim passed for MonoAgent's own label: %+v", p.PreparedBy)
	}
}
