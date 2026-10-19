package library

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An older library login in a profile's vault is the user's real monoes.me login, and the OS key
// store opens it in any copy of the database that the same OS user makes. So it is presented to
// monoes.me (exchanged, refreshed, sent with a request or revoked) only from this OS user's own
// install (#878): OlderLoginPath.

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

// userHome is the home directory the system's user database gives this OS user, never $HOME
// (systemHome, by build). A test replaces it (SetUserHomeForTest).
var userHome = systemHome

// OlderLoginPath says where the older login for host may be read from, given the database a
// command was pointed at (dbPath, expanded), or why it may not be read at all. For a loopback host
// that is dbPath. For any other host it is this OS user's own .monoagent/monoagent.db, under the
// home directory the system gives the user, and only when dbPath is that file and $HOME, where the
// session is stored, is that directory: a copy of the database anywhere else, or a HOME made for a
// test, holds a login that is not its to present. A user the system has no home directory for is
// refused too. The caller reads from the path returned, not from dbPath, so that a path changed
// after the check cannot swap another file in.
func OlderLoginPath(host, dbPath string) (path, why string) {
	if !isProductionHost(host) {
		return dbPath, ""
	}
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
	want := filepath.Join(own, ".monoagent", "monoagent.db")
	if !sameFile(dbPath, want) {
		return "", fmt.Sprintf("the database is %s, not %s", dbPath, want)
	}
	return want, ""
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

// homeInPasswd is the home directory of uid in an /etc/passwd file, "" when it has no entry.
func homeInPasswd(r io.Reader, uid string) string {
	s := bufio.NewScanner(r)
	for s.Scan() {
		f := strings.Split(s.Text(), ":")
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

// SetUserHomeForTest makes the older-login checks take dir as the home directory the system gives
// this OS user, for the rest of the test. It panics outside a test binary, and in a test that calls
// t.Parallel() (t.Setenv does).
func SetUserHomeForTest(t testing.TB, dir string) {
	t.Helper()
	if !testing.Testing() {
		panic("library.SetUserHomeForTest called outside a test binary")
	}
	t.Setenv("MONOAGENT_TEST_USER_HOME", dir) // only for t.Setenv's refusal of a parallel test
	prev := userHome
	userHome = func() (string, error) { return dir, nil }
	t.Cleanup(func() { userHome = prev })
}

// SetProductionHostForTest makes host count as the real monoes.me for the rest of the test, as a
// fake one on a loopback address otherwise does not. Same rules as SetUserHomeForTest.
func SetProductionHostForTest(t testing.TB, host string) {
	t.Helper()
	if !testing.Testing() {
		panic("library.SetProductionHostForTest called outside a test binary")
	}
	t.Setenv("MONOAGENT_TEST_PRODUCTION_HOST", host)
	prev := isProductionHost
	isProductionHost = func(h string) bool { return h == host || prev(h) }
	t.Cleanup(func() { isProductionHost = prev })
}
