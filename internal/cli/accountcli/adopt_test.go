package accountcli

import (
	"context"
	"database/sql"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/account/accounttest"
	"github.com/monoes/mono-agent/internal/cli/accountgate"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
)

// adoptTree is a small tree of the gate's classes: version is open, workflow list gated, daemon serves.
func adoptTree() *cobra.Command {
	root := &cobra.Command{Use: "monoagentcli"}
	root.PersistentFlags().String("db-path", "", "")
	run := func(*cobra.Command, []string) {}
	wf := &cobra.Command{Use: "workflow"}
	wf.AddCommand(&cobra.Command{Use: "list", Run: run})
	root.AddCommand(&cobra.Command{Use: "version", Run: run}, wf, &cobra.Command{Use: "daemon", Run: run})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	accountgate.Apply(root)
	return root
}

func find(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	c, _, err := adoptTree().Find(args)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// counter is an Adopt that counts its calls.
func counter(n *int) Adopt {
	return func(context.Context, *sql.DB, *account.Guard) (bool, error) { *n++; return false, nil }
}

// These install the process guard (accounttest), so they run one at a time.

func TestAdoptFirstRunTriesOncePerDatabase(t *testing.T) {
	accounttest.Install(t, accounttest.LockedNoLogin)
	account.SetEnforceFromForTest(t, time.Now().Add(48*time.Hour))
	path := testdb.Path(t)
	calls := 0
	AdoptFirstRun(find(t, "workflow", "list"), path, counter(&calls))
	AdoptFirstRun(find(t, "daemon"), path, counter(&calls))
	if calls != 1 {
		t.Fatalf("%d tries, want 1", calls)
	}
	db, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v, _ := db.GetSetting(AdoptionSetting); v == "" {
		t.Error("the try is not recorded")
	}
}

func TestAdoptFirstRunSkips(t *testing.T) {
	for _, c := range []struct {
		name string
		mode accounttest.Mode
		args []string
		noDB bool
	}{
		{"an open command", accounttest.LockedNoLogin, []string{"version"}, false},
		{"the gate is dormant", accounttest.Dormant, []string{"workflow", "list"}, false},
		{"a session exists", accounttest.SignedIn, []string{"workflow", "list"}, false},
		{"no database yet", accounttest.LockedNoLogin, []string{"workflow", "list"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			accounttest.Install(t, c.mode)
			if c.mode == accounttest.LockedNoLogin {
				account.SetEnforceFromForTest(t, time.Now().Add(48*time.Hour))
			}
			path := testdb.Path(t)
			if c.noDB {
				path = filepath.Join(t.TempDir(), "absent.db")
			}
			calls := 0
			AdoptFirstRun(find(t, c.args...), path, counter(&calls))
			if calls != 0 {
				t.Errorf("%d tries", calls)
			}
		})
	}
}

// AdoptBeforeGate finds the command and its --db-path on the probe tree and skips help and the root.
func TestAdoptBeforeGateFindsTheCommandAndItsDatabase(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args       []string
		cmd, dbArg string
	}{
		{[]string{"--db-path", "/x.db", "workflow", "list"}, "list", "/x.db"},
		{[]string{"daemon"}, "daemon", ""},
		{[]string{"workflow", "list", "--help"}, "", ""},
		{[]string{"workflow", "list", "-h"}, "", ""},
		{[]string{"workflow", "list", "--bogus"}, "", ""},
		{[]string{}, "", ""},
		{[]string{"nope"}, "", ""},
	} {
		gotCmd, gotDB := "", ""
		AdoptBeforeGate(context.Background(), c.args, adoptTree, func(cmd *cobra.Command, dbPath string) {
			gotCmd, gotDB = cmd.Name(), dbPath
			if cmd.Context() == nil {
				t.Errorf("%v: no context", c.args)
			}
		})
		if gotCmd != c.cmd || gotDB != c.dbArg {
			t.Errorf("%v: %q %q, want %q %q", c.args, gotCmd, gotDB, c.cmd, c.dbArg)
		}
	}
}
