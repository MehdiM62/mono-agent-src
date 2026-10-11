package main

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/account"
)

// ownerDatePlaceholder stands in the CHANGELOG for the enforcement date until the
// owner sets it (internal/account/rollout.go). It must be replaced, at cut time, by
// the date itself.
const ownerDatePlaceholder = "<ENFORCEMENT DATE: owner to set>"

// release.yml takes a release's notes from its own `## [x.y.z]` section, so the
// account story has to be in the notes that ship with release R: in [Unreleased]
// until the cut, in R's section after it. This looks for it above the section that
// was newest before R ([0.100.1]), so later releases add sections above it without
// breaking the test.
//
// While account.EnforceDate() is the zero time the notes must carry the owner
// placeholder, and no date; once a date is set they must name it (UTC, YYYY-MM-DD)
// and no placeholder may remain.
func TestChangelogNamesTheEnforcementDate(t *testing.T) {
	text := docText(t, "CHANGELOG.md")
	if i := strings.Index(text, "\n## [0.100.1]"); i >= 0 {
		text = text[:i]
	}
	notes := flat(text)
	if date := account.EnforceDate(); date.IsZero() {
		if !strings.Contains(text, ownerDatePlaceholder) {
			t.Errorf("account.EnforceDate() is the zero time: the account notes must carry the placeholder %q until the owner sets the date", ownerDatePlaceholder)
		}
	} else {
		if iso := date.UTC().Format("2006-01-02"); !strings.Contains(notes, iso) {
			t.Errorf("no release section above [0.100.1] names the enforcement date %s", iso)
		}
		if strings.Contains(text, ownerDatePlaceholder) {
			t.Errorf("the enforcement date is set: replace %q in CHANGELOG.md", ownerDatePlaceholder)
		}
	}
	for _, section := range strings.Split("\n"+text, "\n## [")[1:] {
		section = "## [" + section
		wants := []string{"a monoes.me account will be required", "account login", "login_required", graceText(), "daemon restart", "-tags devaccount", "SECURITY.md"}
		var missing []string
		for _, w := range wants {
			if !strings.Contains(flat(section), flat(w)) {
				missing = append(missing, w)
			}
		}
		if len(missing) == 0 {
			return
		}
		if strings.HasPrefix(section, "## [Unreleased]") {
			t.Errorf("the [Unreleased] notes lack %q", missing)
		}
	}
	t.Error("CHANGELOG.md has no section above [0.100.1] that tells the account story (release.yml uses a release's section as its notes)")
}
