package workflow

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/testdb"
)

// Send items (send:<id>) and browser submit items (browser:<id>) have no
// execution behind them; the orphan cleanup of a prune must leave them, and
// still remove a real orphan.
func TestPruneKeepsHILItemsWithNoExecution(t *testing.T) {
	db := testdb.Open(t).DB
	s := NewSQLiteWorkflowStore(db)
	for _, row := range [][2]string{
		{"send-item", "send:send-item"},
		{"submit-item", "browser:submit-item"},
		{"orphan", "no-such-execution"},
	} {
		if _, err := db.Exec(`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name) VALUES (?, ?, '', 'n', 'n')`,
			row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PruneExecutions(context.Background(), "wf", 10); err != nil {
		t.Fatal(err)
	}
	left := map[string]bool{}
	rows, err := db.Query(`SELECT id FROM hil_pending`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left[id] = true
	}
	if !left["send-item"] || !left["submit-item"] || left["orphan"] || len(left) != 2 {
		t.Fatalf("after prune: %v, want send-item and submit-item only", left)
	}
}
