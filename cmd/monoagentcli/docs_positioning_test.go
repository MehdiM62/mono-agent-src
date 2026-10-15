package main

import (
	"strings"
	"testing"
)

func TestPositioningTextsAreTrue(t *testing.T) {
	for _, rel := range []string{
		"docs/COMPARISON.md", "docs/USAGE_POLICY.md", "docs/plans/workflow-marketplace-curation-policy.md",
		"CLAUDE.md", ".agents/shared_instructions.md", ".github/ISSUE_TEMPLATE/feature_request.yml",
		"scripts/install-linux-desktop.sh", "wails-app/wails.json",
		"internal/i18n/locales/en.json", "internal/i18n/locales/es.json",
	} {
		assertNoStaleClaims(t, rel)
	}
	// The root help is what a signed-out user reads: it points at the way in and
	// at help that stays open signed out (account --help), never at `ref account`,
	// which is gated like every ref page.
	for _, rel := range []string{"internal/i18n/locales/en.json", "internal/i18n/locales/es.json"} {
		assertSays(t, rel, "account login", "account --help")
		if strings.Contains(docText(t, rel), "ref account") {
			t.Errorf("%s sends a signed-out reader to `ref account`, which is closed until sign-in", rel)
		}
	}
	assertSays(t, "docs/COMPARISON.md", "SECURITY.md#network-use-telemetry-and-crash-reporting", "monoes.me sign-in", "24 hours")
	assertSays(t, "CLAUDE.md", "-tags devaccount")
}

func TestDockerFilesSetUpTheSignIn(t *testing.T) {
	assertSays(t, "docker-compose.yml",
		"MONOAGENT_ALLOW_FILE_KEYRING", "MONOAGENT_FILE_KEYRING_PASSPHRASE_FILE", "account login --email", "account status")
	assertSays(t, "Dockerfile", "monoes.me sign-in")
}
