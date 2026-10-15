package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/cli/profilegate"
	"github.com/monoes/mono-agent/internal/storage"
)

// In ModeEnforce the gate refuses a scoped command run without --profile
// before any of its work, with the CLI's invalid-input exit code.
func TestProfileGateEnforceEndToEnd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dbPath := filepath.Join(t.TempDir(), "gate.db")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO profiles (id, name, created_at) VALUES ('p-work', 'work', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	old := profileGateMode
	profileGateMode = profilegate.ModeEnforce
	t.Cleanup(func() { profileGateMode = old })

	run := func(args ...string) (string, string, error) {
		root := newRootCmd()
		var errOut bytes.Buffer
		root.SetErr(&errOut)
		root.SetArgs(append([]string{"--db-path", dbPath}, args...))
		var runErr error
		out := captureStdout(t, func() { runErr = root.Execute() })
		return out, errOut.String(), runErr
	}

	out, _, err := run("people", "list")
	if err == nil || exitCodeFor(err) != 3 {
		t.Fatalf("people list without --profile: err %v (code %d), want exit 3", err, exitCodeFor(err))
	}
	if !strings.Contains(err.Error(), "--profile") || !strings.Contains(err.Error(), "work (p-work)") {
		t.Errorf("error does not name the profiles: %v", err)
	}
	if out != "" {
		t.Errorf("the refused command printed %q", out)
	}
	if _, _, err := run("config", "list"); err != nil {
		t.Errorf("config list (global): %v", err)
	}
	if _, _, err := run("people"); err != nil {
		t.Errorf("bare people (help): %v", err)
	}
	if _, _, err := run("people", "list", "--profile", "work"); err != nil {
		t.Errorf("people list --profile work: %v", err)
	}
}
