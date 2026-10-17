package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/profileorder"
)

// ErrNoProfile is returned when a profile-scoped call names no profile.
// There is no implicit default: callers (and the UI) test errors.Is(err,
// ErrNoProfile) to say "pass --profile". It is profiledir.ErrNoProfile.
var ErrNoProfile = profiledir.ErrNoProfile

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
	rows, err := d.DB.QueryContext(ctx, `SELECT id, created_at FROM profiles`)
	if err != nil {
		return "", fmt.Errorf("storage: finding the profile for ownerless data: %w", err)
	}
	defer rows.Close()
	type row struct{ id, created string }
	var found []row
	for rows.Next() {
		var r row
		var created sql.NullString
		if err := rows.Scan(&r.id, &created); err != nil {
			return "", fmt.Errorf("storage: finding the profile for ownerless data: %w", err)
		}
		r.created = created.String
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("storage: finding the profile for ownerless data: %w", err)
	}
	if len(found) == 0 {
		return "", nil
	}
	// created_at has two stored formats, so the oldest is picked by
	// profileorder, not by a SQL text compare; 'default' always wins.
	for _, r := range found {
		if r.id == "default" {
			return r.id, nil
		}
	}
	profileorder.Sort(found, func(r row) string { return r.created }, func(r row) string { return r.id })
	return found[0].id, nil
}
