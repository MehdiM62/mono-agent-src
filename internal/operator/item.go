package operator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/commsync"
	"github.com/monoes/mono-agent/internal/hilsend"
)

// ItemRefusal reads HIL item id of profileID and, when only the operator
// decides it, refuses it to a path an agent can reach (the MCP server's and
// the HTTP API's hil tools): a browser form submit or a site an agent asked
// to use (internal/browserctl), a send (internal/hilsend), or a workflow
// Human-in-Loop item whose approval lets a message to a person go out
// (commsync.OutboundHIL). Any other workflow item, and one that isn't there
// (the caller reports that), give nil.
func ItemRefusal(ctx context.Context, db *sql.DB, profileID, id string) error {
	var nc string
	err := db.QueryRowContext(ctx, `SELECT COALESCE(node_config, '') FROM hil_pending WHERE id = ? AND profile_id = ?`, id, profileID).Scan(&nc)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read HIL item %s: %w", id, err)
	case browserctl.IsSubmitItem(nc), browserctl.IsSiteItem(nc), hilsend.IsSend(nc):
		return &ItemError{ID: id, Profile: profileID}
	}
	outbound, err := commsync.OutboundHIL(ctx, db, id, nc)
	if err != nil {
		return err
	}
	if outbound {
		return &ItemError{ID: id, Profile: profileID, Message: true}
	}
	return nil
}

// ItemError is ItemRefusal's refusal. Message says the item lets a
// message to a person go out.
type ItemError struct {
	ID, Profile string
	Message     bool
}

func (e *ItemError) Error() string {
	what := "is for the user to decide, not an agent"
	if e.Message {
		what = "lets a message to a person go out once approved, so the user decides it, not an agent"
	}
	return fmt.Sprintf("HIL item %s %s: the user approves or rejects it in their own terminal "+
		"(`monoagentcli %s hil approve %s`, or `hil reject %s`) or on the Human in Loop page", e.ID, what, browserctl.ProfileFlag(e.Profile), e.ID, e.ID)
}
