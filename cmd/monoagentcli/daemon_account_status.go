package main

import (
	"context"
	"database/sql"

	"github.com/monoes/mono-agent/internal/account"
	agentcli "github.com/monoes/mono-agent/internal/cli/agent"
)

// startAutoRevalidation runs the daemon's automatic re-validation of stale roster entries
// (agentcli.StartAutoRevalidation) with busyOrLocked as its Busy check.
func startAutoRevalidation(ctx context.Context, db *sql.DB, logf func(string, ...interface{})) (wait func()) {
	return agentcli.StartAutoRevalidation(ctx, db, logf, busyOrLocked)
}

// busyOrLocked is the Busy check of the daemon's automatic re-validation with
// the account folded in. A locked account starts nothing, and a model check that
// was refused would be stored as a failed validation, so the scheduler waits.
func busyOrLocked(ctx context.Context, db *sql.DB) (bool, string) {
	if !account.CurrentStatus().Allowed() {
		return true, "the monoes.me account is locked"
	}
	return agentcli.AppBusy(ctx, db)
}
