package workflow

import (
	"errors"
	"fmt"

	"github.com/monoes/mono-agent/internal/storage"
)

// ErrNoProfile is returned when a profile-scoped call names no profile. It is
// the same value as storage.ErrNoProfile, so errors.Is matches either; callers
// and the UI test it to say "pass --profile".
var ErrNoProfile = storage.ErrNoProfile

// ErrLegacyWorkflowNoProfile marks a workflow that carries no profile id. After
// migration 099 and the file-store stamp none should exist; one that does is an
// error state, not "accessible by anyone" and not "the default profile's".
var ErrLegacyWorkflowNoProfile = errors.New("legacy workflow without a profile")

// OwnedBy is the one rule for "does this workflow belong to profileID", shared
// by the CLI, MCP and HTTP front ends and the engine:
//
//   - an empty profileID is ErrNoProfile (there is no default to fall back on);
//   - a workflow with an empty ProfileID is ErrLegacyWorkflowNoProfile;
//   - otherwise it is a plain comparison.
//
// A nil workflow is owned by nobody (false, nil) so callers can keep their
// own not-found handling.
func OwnedBy(wf *Workflow, profileID string) (bool, error) {
	if profileID == "" {
		return false, ErrNoProfile
	}
	if wf == nil {
		return false, nil
	}
	if wf.ProfileID == "" {
		return false, fmt.Errorf("workflow %s: %w", wf.ID, ErrLegacyWorkflowNoProfile)
	}
	return wf.ProfileID == profileID, nil
}
