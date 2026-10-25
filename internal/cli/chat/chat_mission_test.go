package chat

import (
	"context"
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
