package workflow

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/testdb"
)

// Send items (send:<id>) and browser submit items (browser:<id>) have no
// execution behind them. A prune keeps one while it is still to be used
// (pending, approved, or a send being sent) and for 7 days after it was
// last decided or used; after that, and for a real orphan, it is removed.
func TestPruneKeepsHILItemsWhileTheyAreNeeded(t *testing.T) {
	db := testdb.Open(t).DB
	s := NewSQLiteWorkflowStore(db)
	rows := []struct {
		id, execID, status, age string
		keep                    bool
	}{
		{"send-pending-old", "send:1", "pending", "-30 days", true},
		{"send-sending-old", "send:2", "sending", "-30 days", true},
		{"send-approved-old", "send:3", "approved", "-30 days", true},
		{"send-sent-recent", "send:4", "sent", "-1 days", true},
		{"send-sent-old", "send:5", "sent", "-8 days", false},
		{"send-failed-old", "send:6", "failed", "-8 days", false},
		{"submit-pending-old", "browser:1", "pending", "-30 days", true},
		{"submit-approved-old", "browser:2", "approved", "-30 days", true},
		{"submit-rejected-recent", "browser:3", "rejected", "-6 days", true},
		{"submit-consumed-old", "browser:4", "consumed", "-8 days", false},
		{"submit-rejected-old", "browser:5", "rejected", "-8 days", false},
		{"orphan", "no-such-execution", "pending", "-1 days", false},
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, updated_at)
			VALUES (?, ?, '', 'n', 'n', ?, datetime('now', ?))`, r.id, r.execID, r.status, r.age); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PruneExecutions(context.Background(), "wf", 10); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM hil_pending WHERE id = ?`, r.id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if (n == 1) != r.keep {
			t.Errorf("%s (%s, updated %s): kept=%v, want %v", r.id, r.status, r.age, n == 1, r.keep)
		}
	}
}
