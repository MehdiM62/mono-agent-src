package secrets

import (
	"context"
	"database/sql"
	"strings"
)

// Agent access to a profile's vault. By default a run of a workflow the
// operator never approved (one the chat, an agent or another workflow wrote
// or changed) reads no @secret reference (RefuseResolving). The profile's
// owner can switch that refusal off for the whole profile; the switch is
// profile_agent_secret_access (migration 110), written only from the
// operator's own terminal (internal/cli/secret). It lifts that one refusal:
// a reference that came from a run's data or an expression stays refused
// (ErrDataReference), and so does a step an AI runs outside any workflow.

// AllowResolving clears RefuseResolving's mark on ctx: references resolve
// again under it.
func AllowResolving(ctx context.Context) context.Context {
	return context.WithValue(ctx, refuseResolvingKey{}, "")
}

// AgentAccess reports whether the profile's owner let agent-written
// workflows read its vault. A database with no such table (a test's schema)
// says no.
func AgentAccess(ctx context.Context, db *sql.DB, profileID string) (bool, error) {
	if db == nil || profileID == "" {
		return false, nil
	}
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM profile_agent_secret_access WHERE profile_id = ?`, profileID).Scan(&n)
	if err != nil && strings.Contains(err.Error(), "no such table") {
		return false, nil
	}
	return n > 0, err
}

// SetAgentAccess switches it on or off for the profile. The caller has
// checked that this is the operator's own terminal.
func SetAgentAccess(ctx context.Context, db *sql.DB, profileID string, on bool) error {
	if on {
		_, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO profile_agent_secret_access (profile_id) VALUES (?)`, profileID)
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM profile_agent_secret_access WHERE profile_id = ?`, profileID)
	return err
}
