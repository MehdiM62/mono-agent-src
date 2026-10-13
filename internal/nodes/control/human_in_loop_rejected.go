package control

import (
	"context"
	"database/sql"

	"github.com/monoes/mono-agent/internal/workflow"
)

// RejectedHandle is core.human_in_loop's opt-in output for rejected items.
// When an edge leaves it, rejected items go there and the node succeeds;
// otherwise a rejection fails the node with workflow.ErrRejected and the run
// ends REJECTED.
const RejectedHandle = "rejected"

// rejectedEdgeWired reports whether the workflow has an edge from this
// node's rejected handle. The node sees no graph, so it reads the saved
// connections (the file store mirrors every workflow into SQLite); a
// database without the table counts as not wired.
func rejectedEdgeWired(ctx context.Context, db *sql.DB, input workflow.NodeInput) bool {
	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM workflow_connections WHERE workflow_id = ? AND source_node_id = ? AND source_handle = ?`,
		input.WorkflowID, input.NodeID, RejectedHandle).Scan(&n)
	return err == nil && n > 0
}

// splitDecided returns the approved items (with each row's edited_data
// applied) and the rejected items (as they came in), in input order. Rows
// are ordered by insertion, matching the input item order.
func splitDecided(items []workflow.Item, rows []hilRow) (approved, rejected []workflow.Item) {
	for i, item := range items {
		out := copyMap(item.JSON)
		if i < len(rows) {
			if rows[i].status == "rejected" {
				rejected = append(rejected, workflow.NewItem(out))
				continue
			}
			for k, v := range rows[i].edited {
				out[k] = v
			}
		}
		approved = append(approved, workflow.NewItem(out))
	}
	return approved, rejected
}
