package main

import (
	"time"

	"github.com/rs/zerolog"

	browserpkg "github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/cli/bridge"
)

// Per-profile browsers: each monoagent profile's browser actions run in the
// browser profile bound to it. Narrowing a bridge and the route hint live in
// internal/cli/bridge (profile.go); here are the CLI's names for them and the
// halves that need the CLI: its bridge and its database.

// bridgeForProfile narrows bridge to the browser bound to profileID (bridge.ForProfile).
func bridgeForProfile(b browserpkg.ExtensionBridge, profileID string) browserpkg.ExtensionBridge {
	return bridge.ForProfile(b, profileID)
}

// setupProfileBridge is setupExtensionBridge narrowed to profileID's browser.
func setupProfileBridge(profileID string, logger zerolog.Logger, wait time.Duration) browserpkg.ExtensionBridge {
	return bridgeForProfile(setupExtensionBridge(logger, wait), profileID)
}

// browserProfile resolves the profile this invocation runs as, i.e.
// --profile (an id or a name) or the active one, to its id. initDB does the
// resolving; a command that never opened the database would otherwise route
// by a raw name, or by nothing.
func browserProfile(cfg *globalConfig) string {
	db, err := initDB(cfg)
	if err != nil {
		return cfg.ProfileID
	}
	db.Close()
	return cfg.ProfileID
}

// routeWaitPoll is how often ensureExtensionConnected re-checks while it
// waits for a profile's browser; a var so tests can shorten it.
var routeWaitPoll = 500 * time.Millisecond

// routeHint is bridge.RouteHint: why no browser may run a bridge narrowed to a profile.
func routeHint(b connChecker) string { return bridge.RouteHint(b) }
