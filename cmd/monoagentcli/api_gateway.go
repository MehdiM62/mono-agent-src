package main

import (
	"database/sql"

	apiconfigcli "github.com/monoes/mono-agent/internal/cli/apiconfig"
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

// savedSettings and savedEnv are the settings saved with `api config` and the environment with
// them under it (apiconfigcli.SavedSettings, SavedEnv).
var (
	savedSettings = apiconfigcli.SavedSettings
	savedEnv      = apiconfigcli.SavedEnv
)
