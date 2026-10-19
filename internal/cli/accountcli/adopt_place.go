package accountcli

import (
	"fmt"

	"github.com/monoes/mono-agent/internal/library"
)

// placeChecked runs between the check of where the database is (library.OlderLoginPath) and its
// opening: a test changes the path there.
var placeChecked = func() {}

// notAdoptedNotice is the line that says an older login is left alone, and why.
func notAdoptedNotice(host, why string) string {
	return fmt.Sprintf("The earlier library login in this database is not adopted as this machine's sign-in to %s: %s. Sign in instead: monoagentcli account login", host, why)
}

// adoptionNotice is the line that says what an adoption made of a profile's older login. It never
// holds a token.
func adoptionNotice(host, profile string, o library.Outcome) string {
	switch o {
	case library.OutcomeAdopted:
		return fmt.Sprintf("Signed in to %s with the earlier library login of profile %q.", host, profile)
	case library.OutcomeDead:
		return fmt.Sprintf("The earlier library login of profile %q is no longer valid at %s (expired, revoked or used up); it was removed.", profile, host)
	case library.OutcomeUnconfirmed:
		return fmt.Sprintf("The answer of %s to the earlier library login of profile %q was lost or could not be stored; the login was removed so that it is never sent again.", host, profile)
	case library.OutcomeRenewed:
		return fmt.Sprintf("%s renewed the earlier library login of profile %q, but no sign-in could be made of it; the login keeps the new tokens.", host, profile)
	}
	return fmt.Sprintf("%s did not take the earlier library login of profile %q (unreachable, or the request was refused); the login is kept.", host, profile)
}
