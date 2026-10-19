package control

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/profiledir"
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
// bubble, when the profile has that org and that chat and the names have
// an id's shape. One no agent's run queues is untagged, a stray org name
// without an agent included, and so is one of an org-started execution:
// those are the org's decisions. A name the profile doesn't have, one over
// 128 characters or one with other characters is dropped.
func TestHILItemsCarryTheAgentsOrgAndChat(t *testing.T) {
	long := strings.Repeat("a", 129)
	coder := func(conv string) map[string]string {
		return map[string]string{hilsend.EnvAgentContext: "coder", hilsend.EnvConversation: conv}
	}
	role := func(org string) map[string]string {
		return map[string]string{"MONOMIND_ORG_ROLE": "dev", "MONOMIND_ORG_NAME": org}
	}
	for _, c := range []struct {
		name       string
		env        map[string]string
		orgStarted bool
		org, chat  interface{}
	}{
		{"a coder chat's agent", coder("conv-7"), false, nil, "conv-7"},
		{"an org's role", role("acme"), false, "acme", nil},
		{"no agent", map[string]string{"MONOMIND_ORG_NAME": "acme", hilsend.EnvConversation: "conv-7"}, false, nil, nil},
		{"an org-started execution", role("acme"), true, nil, nil},
		{"a chat the profile doesn't have", coder("conv-x"), false, nil, nil},
		{"an org the profile doesn't have", role("ghost"), false, nil, nil},
		{"a chat id over 128 characters", coder(long), false, nil, nil},
		{"an org name over 128 characters", role(long), false, nil, nil},
		{"a chat id with a space", coder("conv 7"), false, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			asOperatorProcess(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			db := newAutoTestDB(t)
			profileHas(t, db, []string{"conv-7", long, "conv 7"}, []string{"acme", long})
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

// profileHas gives the default profile these chats and these orgs.
func profileHas(t *testing.T, db *sql.DB, chats, orgs []string) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE ai_chat_conversations (id TEXT PRIMARY KEY, profile_id TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, id := range chats {
		if _, err := db.Exec(`INSERT INTO ai_chat_conversations (id, profile_id) VALUES (?, 'default')`, id); err != nil {
			t.Fatal(err)
		}
	}
	dir := orgdesign.OrgsDir(profiledir.Root(db, "default"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, org := range orgs {
		if err := os.WriteFile(filepath.Join(dir, org+".json"), []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
