package commsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// OutboundKey marks, in a workflow Human-in-Loop item's node_config, an item
// whose approval lets a message to a person go out: the nodes after the
// Human-in-Loop node send (an email, a DM, a comment, a post, a connect or a
// publish, classified the way an agent's sends are). The node writes it, never
// the workflow's author; only the operator decides such an item
// (operator.ItemRefusal, `hil approve|reject`).
const OutboundKey = "outbound"

// OutboundHIL reports whether HIL item id, with node_config nc, lets a
// message to a person go out: marked so when it was queued, or, for an item
// queued before the mark existed, with a Communications row (hil_ref = id),
// which a workflow item only gets when the nodes after it send.
func OutboundHIL(ctx context.Context, db *sql.DB, id, nc string) (bool, error) {
	var cfg map[string]any
	if json.Unmarshal([]byte(nc), &cfg) == nil {
		if marked, _ := cfg[OutboundKey].(bool); marked {
			return true, nil
		}
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM person_messages WHERE hil_ref = ?`, id).Scan(&n); err != nil {
		return false, fmt.Errorf("commsync: is HIL item %s outbound: %w", id, err)
	}
	return n > 0, nil
}
