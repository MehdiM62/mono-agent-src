package operator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/hilsend"
)

// ItemRefusal reads HIL item id of profileID and, when only the operator
// decides it, refuses it to a path an agent can reach (the MCP server's and
// the HTTP API's hil tools): a browser form submit or a site an agent asked
// to use (internal/browserctl), or a send (internal/hilsend). Any other item,
// a workflow's Human-in-Loop node included, and one that isn't there (the
// caller reports that) give nil.
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
	return nil
}

// ItemError is ItemRefusal's refusal.
type ItemError struct{ ID, Profile string }

func (e *ItemError) Error() string {
	return fmt.Sprintf("HIL item %s is for the user to decide, not an agent: the user approves or rejects it in their own terminal "+
		"(`monoagentcli %s hil approve %s`, or `hil reject %s`) or on the Human in Loop page", e.ID, browserctl.ProfileFlag(e.Profile), e.ID, e.ID)
}
