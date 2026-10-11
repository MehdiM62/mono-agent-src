package main

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/account"
)

// dormantClaims are phrases from the time before the enforcement date was set:
// the gate dormant, the account optional or merely announced, the production key
// missing. Once the date is set, none of them is true of any front door.
var dormantClaims = []string{"dormant", "no date is set", "no enforcement date", "enforcement date is unset", "will be required", "optional today",
	"owner placeholder", "owner to set", "key set is currently empty", "keys are not pinned", "key is not pinned", "no production keys", "not pinned yet"}

// assertNoDormantClaims fails for each phrase in text that describes the gate as
// dormant, optional, announced or unkeyed.
func assertNoDormantClaims(t *testing.T, name, text string) {
	t.Helper()
	text = flat(text)
	for _, bad := range dormantClaims {
		if strings.Contains(text, bad) {
			t.Errorf("%s still describes the gate as not live: %q", name, bad)
		}
	}
}

// The front doors say that the gate is live: the enforcement date is set, they
// name it, they no longer call the gate dormant or the account optional, and
// README, AGENTS.md, SECURITY.md and `ref account` say that the production
// signing key is pinned. Changing the date or the key set needs a simultaneous
// review of these documents and of the release notes.
func TestAccountDocsMatchLiveRollout(t *testing.T) {
	date := account.EnforceDate()
	if date.IsZero() {
		t.Fatal("the enforcement date is unset: the gate is dormant again, so the front-door documents and the release notes must say so together")
	}
	iso := date.UTC().Format("2006-01-02")
	files := []string{"README.md", "AGENTS.md", "SECURITY.md", "SUPPORT.md", "CONTRIBUTING.md", "CLAUDE.md", "docs/COMPARISON.md", "docs/USAGE_POLICY.md",
		"CHANGELOG.md", "internal/i18n/locales/en.json", "internal/i18n/locales/es.json", "Dockerfile", "docker-compose.yml", "install.sh"}
	for _, name := range files {
		text := docText(t, name)
		assertNoDormantClaims(t, name, text)
		for _, stale := range []string{"local-first", "works fully offline", "nothing is sent to us", "no outbound calls on its own behalf"} {
			if strings.Contains(flat(text), stale) {
				t.Errorf("%s contains obsolete product claim %q", name, stale)
			}
		}
		if name != "install.sh" && !strings.Contains(text, iso) {
			t.Errorf("%s does not name the enforcement date %s", name, iso)
		}
	}
	for _, name := range []string{"README.md", "AGENTS.md", "SECURITY.md"} {
		if !strings.Contains(flat(docText(t, name)), "production signing key is pinned") {
			t.Errorf("%s does not say that the production signing key is pinned", name)
		}
	}
	// The first, user-visible lines of the manual and of the root help.
	assertSays(t, "internal/i18n/locales/en.json", "The account gate is live", "login_required")
	assertSays(t, "CHANGELOG.md", "A monoes.me sign-in is required since "+iso)
}
