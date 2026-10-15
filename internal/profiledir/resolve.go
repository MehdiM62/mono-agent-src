package profiledir

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// The one rule that turns what a person typed into a profile id, shared by the
// CLI's --profile, the MCP `profile` argument and the HTTP API's path segment.

// ErrUnknownProfile means no profile has that id or name.
var ErrUnknownProfile = errors.New("profile not found (checked both id and name)")

// ErrAmbiguousProfile means a name is shared by several profiles. It is never
// resolved by guessing: the caller must pass the id.
var ErrAmbiguousProfile = errors.New("profile name matches several profiles: pass the id")

// AmbiguousError is the ErrAmbiguousProfile of one name, with the ids that share it.
type AmbiguousError struct {
	Name string
	IDs  []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("profile name %q matches %d profiles (%s): pass the id", e.Name, len(e.IDs), strings.Join(e.IDs, ", "))
}

// Is makes errors.Is(err, ErrAmbiguousProfile) true.
func (e *AmbiguousError) Is(target error) bool { return target == ErrAmbiguousProfile }

// Candidates are the profiles nameOrID can mean: the profile with exactly that
// id if there is one (the reserved machine scope is never a profile id), else
// every profile with that name, case-insensitively, oldest first. Empty input
// names nothing. More than one candidate means the name is ambiguous.
func Candidates(ctx context.Context, db *sql.DB, nameOrID string) ([]string, error) {
	if nameOrID == "" {
		return nil, nil
	}
	if nameOrID != MachineProfileID {
		var id string
		err := db.QueryRowContext(ctx, `SELECT id FROM profiles WHERE id = ?`, nameOrID).Scan(&id)
		if err == nil {
			return []string{id}, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM profiles WHERE LOWER(name) = LOWER(?) ORDER BY created_at, id`, nameOrID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Resolve returns the id of the profile nameOrID names. It fails with
// ErrUnknownProfile when none does, with an *AmbiguousError (ErrAmbiguousProfile)
// when a name is shared, and with the database's error when the lookup fails.
func Resolve(ctx context.Context, db *sql.DB, nameOrID string) (string, error) {
	ids, err := Candidates(ctx, db, nameOrID)
	if err != nil {
		return "", err
	}
	switch len(ids) {
	case 0:
		return "", ErrUnknownProfile
	case 1:
		return ids[0], nil
	}
	return "", &AmbiguousError{Name: nameOrID, IDs: ids}
}

// Describe lists the profiles a caller may pass, "Work (a1), Home (b2)", oldest
// first. It is empty when there are none or the lookup fails.
func Describe(ctx context.Context, db *sql.DB) string {
	rows, err := db.QueryContext(ctx, `SELECT id, name FROM profiles ORDER BY created_at, id`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var id string
		var name sql.NullString
		if rows.Scan(&id, &name) == nil {
			parts = append(parts, fmt.Sprintf("%s (%s)", name.String, id))
		}
	}
	return strings.Join(parts, ", ")
}
