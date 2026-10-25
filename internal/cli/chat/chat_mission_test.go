package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/tasks"
	"github.com/monoes/mono-agent/internal/testdb"
)

func TestMissionConfigGates(t *testing.T) {
	db := testdb.Open(t).DB
	ctx := context.Background()
	for _, lvl := range []string{"scoped", "read"} {
		if m := missionConfig(ctx, db, "default", "c1", coderTurn{access: lvl, missionConfirmed: true}); m != nil {
			t.Errorf("access %q got a mission setup", lvl)
		}
	}
	if m := missionConfig(ctx, db, "default", "c1", coderTurn{access: "coding"}); m == nil || m.Confirmed || m.Planning() {
		t.Errorf("unconfirmed coding: %+v", m)
	}
	if m := missionConfig(ctx, db, "default", "c1", coderTurn{access: "workspace", missionConfirmed: true}); m == nil || !m.Planning() {
		t.Errorf("confirmed workspace: %+v", m)
	}
	// An open session of the conversation keeps it confirmed on a later turn.
	if _, err := tasks.NewStore(db).CreateSession(ctx, "default", tasks.SessionInput{Title: "M", ConversationID: "c1"}, tasks.Actor{Kind: tasks.Human}); err != nil {
		t.Fatal(err)
	}
	if m := missionConfig(ctx, db, "default", "c1", coderTurn{access: "coding"}); m == nil || !m.Planning() {
		t.Errorf("open session did not confirm: %+v", m)
	}
	if m := missionConfig(ctx, db, "default", "c2", coderTurn{access: "coding"}); m == nil || m.Planning() {
		t.Errorf("another conversation confirmed: %+v", m)
	}
}

func TestMissionRunOnlyForAConfirmedLead(t *testing.T) {
	db := testdb.Open(t).DB
	ctx := context.Background()
	if mr := missionRun(nil); mr != nil {
		t.Errorf("no mission setup: %+v", mr)
	}
	if mr := missionRun(missionConfig(ctx, db, "default", "c1", coderTurn{access: "coding"})); mr != nil {
		t.Errorf("unconfirmed lead got the mission engine: %+v", mr)
	}
	m := missionConfig(ctx, db, "default", "c1", coderTurn{access: "coding", missionConfirmed: true})
	mr := missionRun(m)
	if mr == nil || mr.Store == nil || mr.ProfileID != "default" || mr.Session != 0 {
		t.Fatalf("confirmed lead: %+v (the session is the conversation's, never preset)", mr)
	}
}

// --mission is the operator-friendly hidden flag the UI click passes: it is registered, hidden,
// and refused outside a coder conversation.
func TestChatMissionFlagIsHiddenAndNeedsAConversation(t *testing.T) {
	cmd := newChatCmd(testDeps(&testConfig{DBPath: t.TempDir() + "/x.db", ProfileID: "default"}))
	f := cmd.Flags().Lookup("mission")
	if f == nil || !f.Hidden {
		t.Fatalf("--mission: %+v", f)
	}
	cmd.SetArgs([]string{"--mission", "--runtime", "fake", "hi"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--mission only applies to a coder conversation") {
		t.Fatalf("--mission outside a conversation: %v", err)
	}
}
