package workflow

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/secrets"
)

// A run that read vault secrets under agent access keeps their values in the
// items it carries across a pause (pending inputs, node outputs, node
// state), and a resumed run needs those items as they were: masking them
// would corrupt later steps. So the paused snapshot of such a run is sealed
// under the profile's vault key instead, and opened on resume.

// sealedResumePrefix is secrets.EncryptBlob's marker.
const sealedResumePrefix = "vaultenc:v1:"

// sealResumeState returns state sealed under profileID's vault key when the
// run read secrets, and state itself otherwise.
func sealResumeState(ctx context.Context, db *sql.DB, profileID string, state []byte, readSecrets bool) (string, error) {
	if !readSecrets {
		return string(state), nil
	}
	if db == nil {
		return "", fmt.Errorf("pause: the paused state holds secret values and there is no vault to seal it with")
	}
	sealed, err := secrets.EncryptBlob(ctx, db, profileID, state)
	if err != nil {
		return "", fmt.Errorf("pause: the paused state holds secret values and could not be sealed: %w", err)
	}
	return sealed, nil
}

// openResumeState reverses sealResumeState; an unsealed state comes back as it is.
func openResumeState(ctx context.Context, db *sql.DB, profileID, raw string) ([]byte, error) {
	if !strings.HasPrefix(raw, sealedResumePrefix) {
		return []byte(raw), nil
	}
	if db == nil {
		return nil, fmt.Errorf("resume: the paused state is sealed and there is no vault to open it with")
	}
	return secrets.DecryptBlob(ctx, db, profileID, raw)
}
