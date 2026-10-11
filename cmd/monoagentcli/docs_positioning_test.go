package main

import "testing"

func TestPositioningTextsAreTrue(t *testing.T) {
	for _, rel := range []string{
		"docs/COMPARISON.md", "docs/USAGE_POLICY.md", "docs/plans/workflow-marketplace-curation-policy.md",
		"CLAUDE.md", ".agents/shared_instructions.md", ".github/ISSUE_TEMPLATE/feature_request.yml",
		"scripts/install-linux-desktop.sh", "wails-app/wails.json",
		"internal/i18n/locales/en.json", "internal/i18n/locales/es.json",
	} {
		assertNoStaleClaims(t, rel)
	}
	assertSays(t, "internal/i18n/locales/en.json", "account login", "ref account")
	assertSays(t, "internal/i18n/locales/es.json", "account login", "ref account")
	assertSays(t, "docs/COMPARISON.md", "SECURITY.md#network-use-telemetry-and-crash-reporting", "monoes.me sign-in", "24 hours")
	assertSays(t, "CLAUDE.md", "-tags devaccount")
}

func TestDockerFilesSetUpTheSignIn(t *testing.T) {
	assertSays(t, "docker-compose.yml",
		"MONOAGENT_ALLOW_FILE_KEYRING", "MONOAGENT_FILE_KEYRING_PASSPHRASE_FILE", "account login --email", "account status")
	assertSays(t, "Dockerfile", "monoes.me sign-in")
}
