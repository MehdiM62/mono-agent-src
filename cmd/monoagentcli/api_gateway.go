package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"

	"github.com/monoes/mono-agent/internal/apiconfig"
	"github.com/monoes/mono-agent/internal/cli/apiruntime"
)

// Environment variables of the dedicated /v1 listener's TLS certificate.
const (
	apiTLSCertEnv = apiruntime.TLSCertEnv
	apiTLSKeyEnv  = apiruntime.TLSKeyEnv
)

// apiFlags are the options `httpapi` and `daemon` share for the
// OpenAI-compatible API (/v1).
type apiFlags = apiruntime.Flags

// apiRuntime is the gateway's settings and how its listeners are set up (see
// internal/cli/apiruntime).
type apiRuntime = apiruntime.Runtime

// apiRuntimeDeps are the CLI's pieces the API runtime uses.
func apiRuntimeDeps() apiruntime.Deps {
	return apiruntime.Deps{Version: getVersion, InvalidInput: errInvalidInput, AsCLIError: asCLIError}
}

// newAPIRuntime reads the flags, falling back to the environment and then to the settings
// saved with `api config` (apiruntime.New).
func newAPIRuntime(db *sql.DB, f apiFlags, logf func(format string, args ...any)) (*apiRuntime, error) {
	return apiruntime.New(db, f, logf, apiRuntimeDeps())
}

// daemonAPIRuntime is newAPIRuntime for the daemon: settings saved with `api config` that
// cannot be used turn the API off instead of stopping the daemon (apiruntime.NewForDaemon).
func daemonAPIRuntime(db *sql.DB, f apiFlags, logf func(format string, args ...any)) (*apiRuntime, error) {
	return apiruntime.NewForDaemon(db, f, logf, apiRuntimeDeps())
}

// savedSettings loads the settings saved with `api config` for whatever stands for a server
// started now (the server, `api models`, `api status`). Settings that fail their rules are
// invalid input (exit 3), as a bad flag is; a document that cannot be read is an error.
func savedSettings(ctx context.Context, db *sql.DB) (apiconfig.Settings, error) {
	saved, err := apiconfig.LoadValid(ctx, db)
	return saved, asCLIError(err)
}

// savedEnv is the process environment with the saved settings under it: what a server started
// now would read in its place.
func savedEnv(ctx context.Context, db *sql.DB) (func(string) string, error) {
	saved, err := savedSettings(ctx, db)
	if err != nil {
		return nil, err
	}
	return apiconfig.Overlay(saved, os.Getenv), nil
}

// daemonRoutes is what the daemon's HTTP API server mounts through its single
// ExtraRoutes slot: the org endpoint receiver and, on a loopback bind, the
// OpenAI-compatible API (apiruntime.DaemonRoutes).
func daemonRoutes(orgReceiver func(*http.ServeMux), api *apiRuntime, addr string) func(*http.ServeMux) {
	return apiruntime.DaemonRoutes(orgReceiver, api, addr)
}
