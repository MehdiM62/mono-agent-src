package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The owner's choice (plan B5d, Task 3). These three lines are the only ones
// this file's owner edits by hand.
const (
	licenseName   = "Monoes Proprietary Software License"
	copyrightLine = "© 2026 Nokhodian UG (haftungsbeschränkt)"
	oldLicense    = "MIT"
)

var oldLicenseWord = regexp.MustCompile(`\b` + oldLicense + `\b`)

// licenseDoc reads a file of the repository whose license statements this test keeps in agreement.
func licenseDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// licenseSays checks that a file holds each statement, ignoring case and the line
// breaks of a wrapped paragraph.
func licenseSays(t *testing.T, rel string, wants ...string) {
	t.Helper()
	flat := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	text := flat(licenseDoc(t, rel))
	for _, w := range wants {
		if !strings.Contains(text, flat(w)) {
			t.Errorf("%s does not say %q", rel, w)
		}
	}
}

func TestLicenseMentionsAgree(t *testing.T) {
	if strings.HasPrefix(strings.TrimSpace(licenseDoc(t, "LICENSE")), oldLicense+" License") {
		t.Errorf("LICENSE still holds the %s text", oldLicense)
	}
	licenseSays(t, "LICENSE", strings.ToUpper(licenseName), "Nokhodian UG (haftungsbeschränkt)", "up to and including v0.133.0")
	// README's License section names MIT once, for the earlier versions the license itself lists.
	for _, rel := range []string{"docs/USAGE_POLICY.md", "internal/httpapi/openapi.yaml", "wails-app/wails.json", "docs/COMPARISON.md"} {
		text := licenseDoc(t, rel)
		if rel == "docs/COMPARISON.md" {
			// The table names the competitors' licenses; only our own row is asserted.
			continue
		}
		if oldLicenseWord.MatchString(text) {
			t.Errorf("%s still names %s as the license", rel, oldLicense)
		}
	}
	readme := licenseDoc(t, "README.md")
	if n := len(oldLicenseWord.FindAllString(readme, -1)); n != 1 {
		t.Errorf("README.md names %s %d times, want once (the earlier versions)", oldLicense, n)
	}
	licenseSays(t, "README.md", "licensed under the "+licenseName, "[License: "+licenseName+"]", "publish the results")
	licenseSays(t, "docs/COMPARISON.md", "| **License** | Proprietary")
	licenseSays(t, "internal/httpapi/openapi.yaml", "name: "+licenseName)
	licenseSays(t, "wails-app/wails.json", copyrightLine)
	licenseSays(t, "CONTRIBUTING.md", "## Contribution terms")
}
