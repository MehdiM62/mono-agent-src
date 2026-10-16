package testhome

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { Main(m) }

func TestHomeIsATempDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(home, "monoagent-testhome-") {
		t.Fatalf("HOME = %q, want the temp test home", home)
	}
	if os.Getenv("GOCACHE") == "" || strings.HasPrefix(os.Getenv("GOCACHE"), home) {
		t.Fatalf("GOCACHE = %q, want the real build cache", os.Getenv("GOCACHE"))
	}
	cfg, err := os.UserConfigDir()
	if err != nil || !strings.HasPrefix(cfg, home) {
		t.Fatalf("UserConfigDir = %q (%v), want one inside the test home %s", cfg, err, home)
	}
	for _, k := range []string{"XDG_DATA_HOME", "XDG_STATE_HOME"} {
		if !strings.HasPrefix(os.Getenv(k), home) {
			t.Fatalf("%s = %q, want one inside the test home", k, os.Getenv(k))
		}
	}
}

func TestDefaultTransportRefusesTheNetwork(t *testing.T) {
	if os.Getenv(AllowNetworkEnv) == "1" {
		t.Skip(AllowNetworkEnv + " is set")
	}
	_, err := http.Get("http://example.invalid/")
	if err == nil || !strings.Contains(err.Error(), AllowNetworkEnv) {
		t.Fatalf("err = %v, want the testhome network refusal", err)
	}
	// Transports cloned from the default inherit the guard.
	c := &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	if _, err := c.Get("https://203.0.113.1/"); err == nil || !strings.Contains(err.Error(), AllowNetworkEnv) {
		t.Fatalf("cloned transport err = %v, want the testhome network refusal", err)
	}
}

func TestDefaultTransportReachesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, a := range []string{"localhost:80", "[::1]:443", "0.0.0.0:1", "foo.localhost:1"} {
		if !isLoopback(a) {
			t.Errorf("isLoopback(%q) = false", a)
		}
	}
	for _, a := range []string{"monoes.me:443", "10.0.0.1:80", "localhost.example.com:80"} {
		if isLoopback(a) {
			t.Errorf("isLoopback(%q) = true", a)
		}
	}
}

func TestKeepHomeOnlyUnderTheTempDir(t *testing.T) {
	// t.TempDir is under GOTMPDIR when that is set, else under TMPDIR.
	inside := t.TempDir()
	viaTMPDIR, err := os.MkdirTemp("", "keep-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(viaTMPDIR) })
	for _, dir := range []string{inside, viaTMPDIR} {
		if !keepable(dir) {
			t.Errorf("%s is under the temp dir (TMPDIR %s, GOTMPDIR %q): it may be kept", dir, os.TempDir(), os.Getenv("GOTMPDIR"))
		}
	}
	for _, dir := range []string{"", accountHome(t), "/", os.TempDir(), os.Getenv("GOTMPDIR"), filepath.Join(inside, "missing")} {
		if keepable(dir) {
			t.Errorf("%q must not count as a test home to keep", dir)
		}
	}
}

// A temp dir above the account's home puts that home, and the folders
// between, "under the temp dir": keepable still refuses them, and still
// keeps a folder below the home (TMPDIR=~/scratch/… makes every test home one).
func TestKeepNeverTheAccountHomeOrAbove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the temp dir does not follow TMPDIR on Windows")
	}
	home := accountHome(t)
	below := t.TempDir() // made before the temp dir moves
	parent, top := filepath.Dir(home), filepath.Dir(filepath.Dir(home))
	t.Setenv("TMPDIR", top)
	if !underTempDir(home) {
		t.Fatalf("setup: %s should be under the temp dir %s", home, os.TempDir())
	}
	if keepable(home) {
		t.Errorf("the account's home %s was kept", home)
	}
	if parent != top && keepable(parent) {
		t.Errorf("%s, which holds the account's home, was kept", parent)
	}
	if within(below, home) && !keepable(below) {
		t.Errorf("%s lies below the account's home and under the temp dir: it may be kept", below)
	}
}

func accountHome(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		t.Skipf("no account home: %v", err)
	}
	home, err := filepath.EvalSymlinks(u.HomeDir)
	if err != nil {
		t.Skipf("account home: %v", err)
	}
	return home
}
