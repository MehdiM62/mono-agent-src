package apiconfig

import (
	"context"
	"database/sql"
	"os"

	"github.com/monoes/mono-agent/internal/apiconfig"
)

// SavedSettings loads the settings saved with `api config` for whatever stands for a server
// started now (the server, `api models`, `api status`). Settings that fail their rules are
// invalid input (exit 3), as a bad flag is; a document that cannot be read is an error.
func SavedSettings(ctx context.Context, db *sql.DB) (apiconfig.Settings, error) {
	saved, err := apiconfig.LoadValid(ctx, db)
	return saved, AsCLIError(err)
}

// SavedEnv is the process environment with the saved settings under it: what a server started
// now would read in its place.
func SavedEnv(ctx context.Context, db *sql.DB) (func(string) string, error) {
	saved, err := SavedSettings(ctx, db)
	if err != nil {
		return nil, err
	}
	return apiconfig.Overlay(saved, os.Getenv), nil
}
