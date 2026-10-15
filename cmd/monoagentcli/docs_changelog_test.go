package main

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/account"
)

// ownerDatePlaceholder stood in the CHANGELOG for the enforcement date until the
// owner set it (internal/account/rollout.go). It must never come back.
const ownerDatePlaceholder = "<ENFORCEMENT DATE: owner to set>"

// release.yml takes a release's notes from its own `## [x.y.z]` section, so the
// account story has to be in the notes that ship with release R: in [Unreleased]
// until the cut, in R's section after it. This looks for it above the section that
// was newest before R ([0.100.1]), so later releases add sections above it without
// breaking the test.
//
// The enforcement date must be set (account.EnforceDate()); the notes name it
// (UTC, YYYY-MM-DD) and carry no placeholder.
func TestChangelogNamesTheEnforcementDate(t *testing.T) {
	text := docText(t, "CHANGELOG.md")
	if i := strings.Index(text, "\n## [0.100.1]"); i >= 0 {
		text = text[:i]
	}
	notes := flat(text)
	date := account.EnforceDate()
	if date.IsZero() {
		t.Fatal("account.EnforceDate() is the zero time: the account notes were written for a live gate")
	}
	if iso := date.UTC().Format("2006-01-02"); !strings.Contains(notes, iso) {
		t.Errorf("no release section above [0.100.1] names the enforcement date %s", iso)
	}
	if strings.Contains(text, ownerDatePlaceholder) {
		t.Errorf("the enforcement date is set: remove %q from CHANGELOG.md", ownerDatePlaceholder)
	}
	for _, section := range strings.Split("\n"+text, "\n## [")[1:] {
		section = "## [" + section
		wants := []string{"a monoes.me sign-in is required", "account login", "login_required", graceText(), "daemon restart", "-tags devaccount", "SECURITY.md"}
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
