package hil

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/commsync"
)

// A workflow Human-in-Loop item whose approval lets a message to a person go
// out (commsync.OutboundHIL: an email, a DM, a comment, a post, a connect or a
// publish after the node) is the operator's to decide, like a send: an agent,
// the chat's runs and a workflow's own processes are refused, and the item
// stays pending. Every other workflow item stays open to them.

// requireOperatorIfOutbound refuses all but the operator when id is such an
// item; an item that isn't there is left to the decision to report.
func (d Deps) requireOperatorIfOutbound(cmd *cobra.Command, id string) error {
	db, err := d.OpenDB()
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	defer db.Close()
	var nc string
	err = db.DB.QueryRowContext(cmd.Context(), `SELECT COALESCE(node_config, '') FROM hil_pending WHERE id = ? AND profile_id = ?`,
		id, d.ProfileID()).Scan(&nc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading HIL item %s: %w", id, err)
	}
	outbound, err := commsync.OutboundHIL(cmd.Context(), db.DB, id, nc)
	if err != nil {
		return err
	}
	if outbound {
		return d.RequireOperator("approve or reject a workflow step that sends a message to someone")
	}
	return nil
}
