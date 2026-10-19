package control

import (
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/workflow"
)

// asOperatorProcess clears every marker of an agent's process and the tags
// one carries.
func asOperatorProcess(t *testing.T) {
	t.Helper()
	for _, k := range append(orgsign.AgentContextMarkers(), hilsend.EnvAgentContext, hilsend.EnvConversation, "MONOAGENT_ACTOR",
		"MONOMIND_ORG_NAME") {
		t.Setenv(k, "")
	}
}

// An item an agent's run queues carries the org and the coder chat the
// agent says it works for, as its send items do, so it reaches that
// bubble. One no agent's run queues is untagged, a stray org name without
// an agent included, and so is one of an org-started execution: those are
// the org's decisions.
func TestHILItemsCarryTheAgentsOrgAndChat(t *testing.T) {
	for _, c := range []struct {
		name       string
		env        map[string]string
		orgStarted bool
		org, chat  interface{}
	}{
		{"a coder chat's agent", map[string]string{hilsend.EnvAgentContext: "coder", hilsend.EnvConversation: "conv-7"}, false, nil, "conv-7"},
		{"an org's role", map[string]string{"MONOMIND_ORG_ROLE": "dev", "MONOMIND_ORG_NAME": "acme"}, false, "acme", nil},
		{"no agent", map[string]string{"MONOMIND_ORG_NAME": "acme", hilsend.EnvConversation: "conv-7"}, false, nil, nil},
		{"an org-started execution", map[string]string{"MONOMIND_ORG_ROLE": "dev", "MONOMIND_ORG_NAME": "acme"}, true, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			asOperatorProcess(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			db := newAutoTestDB(t)
			ctx := autoCtx(db)
			if c.orgStarted {
				ctx = workflow.WithTrigger(ctx, workflow.TriggerTypeOrgTool, map[string]interface{}{"org": map[string]interface{}{"name": "acme", "role": "dev"}})
			}
			cfg := map[string]interface{}{"readonly_fields": []interface{}{"to"}, "editable_fields": []interface{}{"caption"}}
			if _, err := (&HumanInLoopNode{}).Execute(ctx, threeItems(), cfg); !errors.Is(err, workflow.ErrNodePaused) {
				t.Fatalf("err = %v, want paused", err)
			}
			for _, r := range rowsOf(t, db) {
				if nc := cfgOf(t, r); nc["org"] != c.org || nc["conversationId"] != c.chat {
					t.Errorf("node_config %v: org %v, conversationId %v; want %v, %v", nc, nc["org"], nc["conversationId"], c.org, c.chat)
				}
			}
		})
	}
}
