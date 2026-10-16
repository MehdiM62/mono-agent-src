// Package testhome keeps a package's tests out of the real home directory.
//
// Many code paths default to ~/.monoagent (the database, profiles, AI tool
// backups), so a test that forgets t.Setenv("HOME", …) writes into the
// user's real state — and can migrate their real database. Call Main from a
// package's TestMain to give the whole test binary a throwaway HOME:
//
//	func TestMain(m *testing.M) { testhome.Main(m) }
package testhome

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Main points HOME (and USERPROFILE) at a fresh temp dir, makes
// http.DefaultTransport refuse non-loopback hosts (see AllowNetworkEnv), runs
// the tests, removes the dir and exits. Go's build and module caches, and the user cache
// dir (rod's browser download), keep their real locations so tests that
// build binaries or launch a browser don't start cold.
func Main(m *testing.M) {
	os.Exit(run(m))
}

// realHome is the home directory as it was before Main replaced it.
var realHome string

// RealHome is the user's own home directory, as it was before Main replaced
// it. Only an opt-in live test (behind a build tag) that reads the user's
// state on purpose uses it; everything else belongs in the test home.
func RealHome() string { return realHome }

// KeepHomeEnv, set to 1 by a test that starts its own test binary as a helper
// process with a HOME of its own (a rig's home), makes Main in that helper keep
// that HOME instead of replacing it. It is honoured only for a HOME under the
// temp dir, so it can never point a test at the user's real home.
const KeepHomeEnv = "MONOAGENT_TESTHOME_KEEP"

func run(m *testing.M) int {
	realHome, _ = os.UserHomeDir()
	keepCaches()
	blockNetwork()
	if home := os.Getenv("HOME"); os.Getenv(KeepHomeEnv) == "1" && underTempDir(home) {
		return m.Run()
	}
	dir, err := os.MkdirTemp("", "monoagent-testhome-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testhome:", err)
		return 1
	}
	defer os.RemoveAll(dir)
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)
	// The config and data dirs follow HOME only when XDG_* is unset; a user
	// who sets them would otherwise have tests write into their real ones.
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	os.Setenv("XDG_DATA_HOME", filepath.Join(dir, ".local", "share"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, ".local", "state"))
	return m.Run()
}

// keepCaches pins cache locations derived from HOME before HOME changes.
func keepCaches() {
	if cache, err := os.UserCacheDir(); err == nil {
		setIfUnset("GOCACHE", filepath.Join(cache, "go-build"))
		if os.Getenv("XDG_CACHE_HOME") == "" && os.Getenv("HOME") != "" {
			// Only Linux and the BSDs read it; harmless elsewhere.
			os.Setenv("XDG_CACHE_HOME", cache)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			gopath = filepath.Join(home, "go")
			os.Setenv("GOPATH", gopath)
		}
		setIfUnset("GOMODCACHE", filepath.Join(filepath.SplitList(gopath)[0], "pkg", "mod"))
	}
}

func setIfUnset(key, value string) {
	if os.Getenv(key) == "" {
		os.Setenv(key, value)
	}
}

// underTempDir reports whether dir lies inside the temp dir (os.TempDir,
// which follows TMPDIR), symlinks resolved.
func underTempDir(dir string) bool {
	if dir == "" {
		return false
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return false
	}
	d, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(tmp, d)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
