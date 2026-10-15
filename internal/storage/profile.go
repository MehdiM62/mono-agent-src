package storage

import "errors"

// ErrNoProfile is returned when a profile-scoped call names no profile.
// There is no implicit default: callers (and the UI) test errors.Is(err,
// ErrNoProfile) to say "pass --profile".
var ErrNoProfile = errors.New("no profile named: pass --profile <id|name>")

// requireProfile returns ErrNoProfile (wrapped with the operation) for an
// empty profile id.
func requireProfile(profileID string) error {
	if profileID == "" {
		return ErrNoProfile
	}
	return nil
}
