package n8nimport

import "testing"

// withoutAppSources hides the registered API package sources (the full
// catalog, when the test binary links noderegistry) for one test.
func withoutAppSources(t *testing.T) {
	t.Helper()
	regMu.Lock()
	saved, savedApps := appSources, srcApps
	appSources, srcApps = nil, map[string]AppMapper{}
	regMu.Unlock()
	t.Cleanup(func() {
		regMu.Lock()
		appSources, srcApps = saved, savedApps
		regMu.Unlock()
	})
}
