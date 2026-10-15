package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrNoProfile is returned when a profile-scoped call names no profile.
// There is no implicit default: callers (and the UI) test errors.Is(err,
// ErrNoProfile) to say "pass --profile".
var ErrNoProfile = errors.New("no profile named: pass --profile <id|name>")

// requireProfile returns ErrNoProfile for an empty profile id.
func requireProfile(profileID string) error {
	if profileID == "" {
		return ErrNoProfile
	}
	return nil
}

// LegacyOwnerProfile returns the profile that inherits data with no owner: the
// profile whose id is 'default' if it exists, else the oldest profile. It is
// "" with a nil error when there are no profiles at all (nothing can inherit).
// Migration 099 and the workflow file-store stamp use this one rule.
func (d *Database) LegacyOwnerProfile(ctx context.Context) (string, error) {
	var id string
	err := d.DB.QueryRowContext(ctx,
		`SELECT id FROM profiles ORDER BY (id = 'default') DESC, created_at ASC, id ASC LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("storage: finding the profile for ownerless data: %w", err)
	}
	return id, nil
}
