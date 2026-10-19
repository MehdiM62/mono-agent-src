package accountcli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/library"
)

// isProductionHost says whether host is the monoes.me whose logins are real: only there does it
// matter which database an adoption reads (#878). Any other host is a devaccount build's
// MONOES_BASE_URL, and a library login is stored for its host, so a copy of a real database holds
// none to present to it. A test replaces it.
var isProductionHost = func(host string) bool { return host == account.HostURL }

// userHome is the home directory the system gives this OS user (os/user), not $HOME. A test
// replaces it (SetUserHomeForTest).
var userHome = func() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

// SetUserHomeForTest makes the adoption take dir as the home directory the system gives this OS
// user, for the rest of the test. It panics outside a test binary, and in a test that calls
// t.Parallel() (t.Setenv does).
func SetUserHomeForTest(t testing.TB, dir string) {
	t.Helper()
	if !testing.Testing() {
		panic("accountcli.SetUserHomeForTest called outside a test binary")
	}
	t.Setenv("MONOAGENT_TEST_USER_HOME", dir) // only for t.Setenv's refusal of a parallel test
	prev := userHome
	userHome = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userHome = prev })
}

// notOwnInstall says why the database at path is not this OS user's own install, "" when it is.
// An adoption presents a real monoes.me login, so it runs only on the database that install uses:
// .monoagent/monoagent.db under the home directory the system gives the user, with $HOME, where
// the session is stored, the same directory. A copy of that database anywhere else, or a HOME made
// for a test, holds a login that is not its to present (#878).
func notOwnInstall(path string) string {
	own, err := userHome()
	if err != nil || own == "" {
		return "the system does not say which home directory this OS user has"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "the home directory is not set"
	}
	if !sameFile(home, own) {
		return fmt.Sprintf("the home directory is %s, not this OS user's %s", home, own)
	}
	if want := filepath.Join(own, ".monoagent", "monoagent.db"); !sameFile(path, want) {
		return fmt.Sprintf("the database is %s, not %s", path, want)
	}
	return ""
}

// sameFile says whether a and b are one file or directory, however each is spelled.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
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
