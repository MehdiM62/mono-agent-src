package hilsend

import (
	"context"
	"database/sql"
	"errors"
)

// ErrPublicationNeedsApproval refuses a publication registered by whatever is
// not the operator's own terminal (an agent, the chat, an AI-started run)
// that names no send the operator approved and that was sent.
var ErrPublicationNeedsApproval = errors.New("registering a publication from an agent needs an approved send: prepare the send (it is queued for approval, and registers itself once approved), or name the id of a send that was approved and sent")

// PublicationApproved checks a publication that whatever is not the
// operator's own terminal registers: with approvals of sends on (Enabled),
// approvalID must name a send of profile that was approved and sent. The
// operator's own registrations need no check, and the caller decides who
// that is.
func PublicationApproved(ctx context.Context, db *sql.DB, profile, approvalID string) error {
	if !Enabled(db) {
		return nil
	}
	if approvalID != "" {
		if p, err := (Store{DB: db, Profile: profile}).Get(ctx, approvalID); err == nil && p.State == StateSent {
			return nil
		}
	}
	return ErrPublicationNeedsApproval
}
