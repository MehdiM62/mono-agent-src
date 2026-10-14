package main

import (
	"context"
	"database/sql"

	"github.com/monoes/mono-agent/internal/account"
)

// busyOrLocked is the Busy check of the daemon's automatic re-validation with
// the account folded in. A locked account starts nothing, and a model check that
// was refused would be stored as a failed validation, so the scheduler waits.
func busyOrLocked(ctx context.Context, db *sql.DB) (bool, string) {
	if !account.CurrentStatus().Allowed() {
		return true, "the monoes.me account is locked"
	}
	return appBusy(ctx, db)
}
