package bridge

import (
	"errors"

	browserpkg "github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/extension"
)

// Per-profile browsers: each monoagent profile's browser actions run in the
// browser profile bound to it (see internal/extension/conns.go for the
// routing rule). These are the CLI's half of it that need nothing of the CLI:
// narrowing a bridge to a profile's browser and explaining why none may run it.

// ForProfile narrows bridge to the browser bound to profileID. A bridge that
// cannot route (a test double), or no profile at all, is returned unchanged,
// which reaches the default browser.
func ForProfile(bridge browserpkg.ExtensionBridge, profileID string) browserpkg.ExtensionBridge {
	if r, ok := bridge.(browserpkg.ProfileRouter); ok && profileID != "" {
		return r.ForProfile(profileID)
	}
	return bridge
}

// RouteHint explains, for a bridge narrowed to a profile, why no browser
// may run it. It is appended to EnsureConnected's timeout errors (Hints.Route),
// which otherwise only say "did not connect".
func RouteHint(bridge ConnChecker) string {
	r, ok := bridge.(interface {
		Route() (extension.ConnInfo, error)
	})
	if !ok {
		return ""
	}
	_, err := r.Route()
	var nb *extension.NoBrowserError
	if errors.As(err, &nb) {
		return "\n" + nb.Error()
	}
	return ""
}
