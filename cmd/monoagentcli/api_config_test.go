package main

// `api config show|set|unset`: the saved settings of the OpenAI-compatible API's server. The
// commands are internal/cli/apiconfig's (tested there); these are the helpers the tests of this
// package share, and the test that the group is wired into `api`.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/apiconfig"
	"github.com/monoes/mono-agent/internal/autostart"
	"github.com/monoes/mono-agent/internal/storage"
)

// No test of this package runs a service manager: `api config` and `daemon restart` get their
// installer from newInstaller, which a test replaces and which defaults here to one that does
// nothing.
func init() {
	newInstaller = func() autostart.Installer { return &fakeAutostart{} }
}

// useInstaller replaces the service manager for one test.
func useInstaller(t *testing.T, in autostart.Installer) {
	t.Helper()
	was := newInstaller
	newInstaller = func() autostart.Installer { return in }
	t.Cleanup(func() { newInstaller = was })
}

// configTest is a database of its own with the API's variables clear and no daemon.
func configTest(t *testing.T) string {
	t.Helper()
	db := newAPITestDB(t)
	clearAPIEnv(t)
	return db
}

func decodeConfig(t *testing.T, out string) apiconfig.ConfigReport {
	t.Helper()
	var r apiconfig.ConfigReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("not an api config document: %v\n%s", err, out)
	}
	return r
}

func decodeChange(t *testing.T, out string) apiconfig.ChangeResult {
	t.Helper()
	var r apiconfig.ChangeResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("not an api config change document: %v\n%s", err, out)
	}
	return r
}

func savedAt(t *testing.T, dbPath string) apiconfig.Settings {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := apiconfig.Load(context.Background(), db.DB)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// What the commands are called, and what they say about themselves.
func TestAPIConfigIsPartOfTheAPICommand(t *testing.T) {
	db := configTest(t)
	out, _, err := runAPI(t, db, "default", false, "config", "set", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--v1-addr", "--tls-cert-file", "--tls-key-file", "--confinement", "--context-confinement", "--auto-confinement",
		"--max-concurrent", "--turn-timeout", "--image-runtimes", "--tool-runtimes", "--yes", "--dry-run"} {
		if !strings.Contains(out, flag) {
			t.Errorf("`api config set --help` does not list %s", flag)
		}
	}
}
