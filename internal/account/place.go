package account

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
)

// The machine session and an older library login are the user's real monoes.me credentials, and
// the OS key store opens them in any copy of ~/.monoagent that the same OS user makes. A copy that
// renews or revokes one races the original: monoes.me ends every refresh token of the account
// when a spent one is presented again. So these are presented to monoes.me only from this OS
// user's own install (#878): a sealed refresh token names the folder it was sealed for (sealedFor),
// and what names no place is read only where OwnHome agrees.

// isProductionHost says whether host may be the real monoes.me. Every host is, however it is
// spelled (https://monoes.me:443, HTTPS://MONOES.ME, https://monoes.me/.), unless it is a loopback
// address: a development server on this machine, where a login is stored for that host alone. A
// test adds a host (SetProductionHostForTest).
var isProductionHost = func(host string) bool {
	u, err := url.Parse(host)
	if err != nil {
		return true
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" {
		return false
	}
	ip := net.ParseIP(h)
	return ip == nil || !ip.IsLoopback()
}

// IsProductionHost says whether host may be the real monoes.me: every host but a loopback address.
func IsProductionHost(host string) bool { return isProductionHost(host) }

// userHome is the home directory the system's user database gives this OS user, never $HOME
// (systemHome, by build). A test replaces it (SetUserHomeForTest).
var userHome = systemHome

// SystemHome is the home directory the system's user database gives this OS user, never $HOME:
// "" or an error when it has none (on Linux without cgo, a user /etc/passwd has no entry for).
func SystemHome() (string, error) { return userHome() }

// OwnHome is the home directory the system gives this OS user, when $HOME is that directory. why
// says otherwise, "" when it is: a HOME made for a test or an agent is not the user's, and neither
// is any HOME of a user the system has no home directory for.
func OwnHome() (own, why string) {
	own, err := userHome()
	if err != nil || own == "" {
		return "", "the system has no home directory for this OS user"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "the home directory is not set"
	}
	if !sameFile(home, own) {
		return "", fmt.Sprintf("the home directory is %s, not this OS user's %s", home, own)
	}
	return own, ""
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

// homeInPasswd is the home directory of uid in an /etc/passwd file, "" when it has no entry. A
// commented-out line is no entry.
func homeInPasswd(r io.Reader, uid string) string {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		f := strings.Split(line, ":")
		if len(f) >= 7 && f[2] == uid {
			return f[5]
		}
	}
	return ""
}

// homeFromPasswd is the home directory of uid in the passwd file at path, with no fallback.
func homeFromPasswd(path, uid string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if h := homeInPasswd(f, uid); h != "" {
		return h, nil
	}
	return "", fmt.Errorf("%s has no entry for uid %s", path, uid)
}

// SetUserHomeForTest makes OwnHome take dir as the home directory the system gives this OS user,
// for the rest of the test; "" is a user the system has none for. Like the other seams, it panics
// outside a test binary and in a test that calls t.Parallel().
func SetUserHomeForTest(t testing.TB, dir string) {
	t.Helper()
	requireTestBinary("SetUserHomeForTest")
	t.Setenv(testStateEnv, "1")
	prev := userHome
	userHome = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userHome = prev })
}

// SetProductionHostForTest makes host count as the real monoes.me for the rest of the test, as a
// fake one on a loopback address otherwise does not. Same rules as SetUserHomeForTest.
func SetProductionHostForTest(t testing.TB, host string) {
	t.Helper()
	requireTestBinary("SetProductionHostForTest")
	t.Setenv(testStateEnv, "1")
	prev := isProductionHost
	isProductionHost = func(h string) bool { return h == host || prev(h) }
	t.Cleanup(func() { isProductionHost = prev })
}
