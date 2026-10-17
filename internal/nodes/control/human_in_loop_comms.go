package control

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// commsItems maps each input item of the node to the outbound message it
// would send once approved (nil where the nodes after it send nothing). The
// engine hands the node what follows it; run on its own there is nothing.
func commsItems(ctx context.Context, input workflow.NodeInput, profileID string) []*commsync.Item {
	name, nodes := commsync.DownstreamFrom(ctx)
	items := make([]map[string]interface{}, len(input.Items))
	for i, it := range input.Items {
		items[i] = it.JSON
	}
	out := hilsend.WorkflowItems(profileID, name, nodes, items)
	for _, it := range out {
		if it == nil {
			continue
		}
		wf, _ := it.Meta["workflow"].(map[string]interface{})
		if wf == nil {
			wf = map[string]interface{}{}
			it.Meta["workflow"] = wf
		}
		wf["id"], wf["execution_id"], wf["hil_node_id"] = input.WorkflowID, input.ExecutionID, input.NodeID
	}
	return out
}

// reachableSends reports whether anything that can run once the node's items
// are approved (commsync.ReachableFrom: every later node, through control
// nodes and called workflows) may send a message to a person, classified as
// an agent's sends are (hilsend.MaySend: an operation only the run resolves
// counts), or the engine could not tell: then it fails closed. Run on its
// own, outside an engine, nothing follows it.
func reachableSends(ctx context.Context) bool {
	r, ok := commsync.ReachableFrom(ctx)
	if !ok {
		return false
	}
	if r.Unresolved {
		return true
	}
	for _, n := range r.Nodes {
		if hilsend.MaySend(n.Type, n.Config) {
			return true
		}
	}
	return false
}

// queueComms puts the item in Communications as soon as its approval is
// queued, and carries an auto-decided outcome over. A failure is logged and
// never blocks the approval.
func queueComms(ctx context.Context, db *sql.DB, it *commsync.Item, hilID, hilStatus string) {
	if it == nil {
		return
	}
	it.HilRef = hilID
	if _, err := commsync.Queue(ctx, db, *it); err != nil {
		log.Warn().Err(err).Str("hil", hilID).Msg("human_in_loop: communications: queue")
		return
	}
	switch hilStatus {
	case "approved":
		_ = commsync.SetStatus(ctx, db, hilID, commsync.Approved, commsync.Detail{})
	case "rejected":
		_ = commsync.SetStatus(ctx, db, hilID, commsync.Rejected, commsync.Detail{})
	}
}

// stalePendingIDs lists the node's pending items older than the timeout.
func stalePendingIDs(ctx context.Context, db *sql.DB, input workflow.NodeInput, minutes float64) []string {
	rows, err := db.QueryContext(ctx,
		`SELECT id FROM hil_pending WHERE execution_id=? AND node_id=? AND status='pending' AND created_at < datetime('now', ?)`,
		input.ExecutionID, input.NodeID, fmt.Sprintf("-%d minutes", int(minutes)))
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
