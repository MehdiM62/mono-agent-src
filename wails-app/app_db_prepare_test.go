//go:build !windows

package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// The app migrates nothing itself: `db migrate` runs in the CLI and the app
// reads its answer.
func TestRunDBMigrateShellsOut(t *testing.T) {
	log := filepath.Join(t.TempDir(), "args.log")
	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, `echo "$*" >> '`+log+`'
echo '{"schema_version":97,"applied":2,"active_profile":"work","profiles":["default","work"],"vault_moved":3,"errors":["profile work: vault file migration: boom"]}'
`))
	res, err := runDBMigrate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.SchemaVersion != 97 || res.Applied != 2 || res.ActiveProfile != "work" || len(res.Profiles) != 2 || res.VaultMoved != 3 || len(res.Errors) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if got := loggedArgs(t, log); strings.Join(got, "\n") != "--json db migrate" {
		t.Fatalf("CLI calls: %q", got)
	}
}

func TestRunDBMigrateFailures(t *testing.T) {
	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, `echo 'Log in to monoes.me first' >&2; exit 4`))
	if _, err := runDBMigrate(context.Background()); !errors.Is(err, errDBMigrateNeedsLogin) {
		t.Fatalf("exit 4: %v", err)
	}

	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, `echo 'Error: unknown command "db" for "monoagentcli"' >&2; exit 1`))
	if _, err := runDBMigrate(context.Background()); err == nil || !strings.Contains(err.Error(), `unknown command "db"`) {
		t.Fatalf("old CLI: %v", err)
	}

	t.Setenv("MONOAGENTCLI_BIN", fakeCLI(t, `echo 'not json'`))
	if _, err := runDBMigrate(context.Background()); err == nil || !strings.Contains(err.Error(), "unexpected output") {
		t.Fatalf("bad output: %v", err)
	}
}

// A migration the gate postponed is retried only once a status allows work,
// and only once.
func TestRetryDBMigrateWaitsForASession(t *testing.T) {
	for _, c := range []struct {
		status string
		allows bool
	}{
		{`{"v":1,"state":"ok"}`, true},
		{`{"v":1,"state":"grace"}`, true},
		{`{"v":1,"state":"locked"}`, false},
		{`{"error":"x","code":"cli_failed"}`, false},
		{`not json`, false},
	} {
		if got := statusAllowsWork(c.status); got != c.allows {
			t.Errorf("statusAllowsWork(%s) = %v", c.status, got)
		}
	}

	a := &App{}
	a.dbMigratePending.Store(true)
	a.retryDBMigrateAfterLogin(`{"v":1,"state":"locked"}`)
	if !a.dbMigratePending.Load() {
		t.Fatal("a locked status cleared the pending migration")
	}
	a.dbMigratePending.Store(false)
	a.retryDBMigrateAfterLogin(`{"v":1,"state":"ok"}`) // nothing pending: no run
	if a.dbMigratePending.Load() {
		t.Fatal("pending set with nothing postponed")
	}
}
