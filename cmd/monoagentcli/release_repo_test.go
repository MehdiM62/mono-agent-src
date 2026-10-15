package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/release"
)

// sourceRepo is the repository the code lives in once it is renamed and
// private (owner/name). No URL that a user's machine or browser uses to get,
// report on or verify a release may name it.
const sourceRepo = "monoes/mono-agent-src"

// releaseDoc reads a file of the repository whose release URLs this test checks.
func releaseDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestReleaseURLsNameOnlyThePublicRepo(t *testing.T) {
	if releasesRepo == sourceRepo {
		t.Fatalf("releasesRepo and sourceRepo are both %q: the releases repository must be a different, public one", sourceRepo)
	}
	if release.ReleasesRepo != releasesRepo {
		t.Errorf("internal/release.ReleasesRepo = %q, want %q (the signed-manifest fallback URL must be the same repository)", release.ReleasesRepo, releasesRepo)
	}
	if want := "https://api.github.com/repos/" + releasesRepo + "/releases/latest"; latestReleaseURL != want {
		t.Errorf("latestReleaseURL = %q, want %q", latestReleaseURL, want)
	}
	for _, rel := range []string{
		"install.sh", "README.md", "SECURITY.md", "SUPPORT.md", ".github/workflows/release.yml",
		"cmd/monoagentcli/update.go", "cmd/monoagentcli/status_db_release_adapter.go", "internal/cli/crashreport/crashreport.go", "internal/release/manifest.go",
		"cmd/release-manifest/main.go", "cmd/release-manifest/check.go", "scripts/release-e2e-test.sh", "docs/release-runbook.md",
	} {
		text := releaseDoc(t, rel)
		for _, bad := range []string{
			"repos/" + sourceRepo, sourceRepo + "/releases", "githubusercontent.com/" + sourceRepo, "-R " + sourceRepo,
			`REPO="` + sourceRepo, sourceRepo + "/discussions", sourceRepo + "/issues", sourceRepo + "/security",
			"github.com/" + sourceRepo,
		} {
			if strings.Contains(text, bad) {
				t.Errorf("%s names the private source repository (%q): users cannot reach it", rel, bad)
			}
		}
	}
	if !strings.Contains(releaseDoc(t, "install.sh"), `REPO="`+releasesRepo+`"`) {
		t.Errorf("install.sh does not install from %s", releasesRepo)
	}
	wf := releaseDoc(t, ".github/workflows/release.yml")
	if !strings.Contains(wf, "RELEASES_REPO: "+releasesRepo+"\n") {
		t.Errorf("release.yml does not publish to %s", releasesRepo)
	}
	for _, bad := range []string{"github.com/monoes/mono-agent/releases", "attest-build-provenance", "id-token:", "attestations:", "secrets.RELEASES_REPO }}"} {
		if strings.Contains(wf, bad) {
			t.Errorf("release.yml still contains %q", bad)
		}
	}
	for _, rel := range []string{"README.md", "SECURITY.md", "SUPPORT.md"} {
		if strings.Contains(releaseDoc(t, rel), "attestation verify") {
			t.Errorf("%s tells users to verify an attestation, which a private build repository cannot provide", rel)
		}
	}
}
