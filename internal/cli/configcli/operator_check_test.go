package configcli

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/storage"
)

// A Deps without its operator check refuses a reserved row with the
// operator-only code (exit 3, operator_only), and says the check is not
// wired; any other row is anyone's (#845 follow-up).
func TestConfigWithoutAnOperatorCheckRefusesReservedRows(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	d := Deps{
		JSONOutput: func() bool { return true },
		OpenDB: func() (*storage.Database, error) {
			db, err := storage.NewDatabase(dbPath)
			if err != nil {
				return nil, err
			}
			return db, db.ApplyMigrations()
		},
	}
	run := func(args ...string) error {
		cmd := NewCommand(d)
		cmd.SetOut(&strings.Builder{})
		cmd.SetErr(&strings.Builder{})
		cmd.SetArgs(args)
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		return cmd.Execute()
	}
	for _, args := range [][]string{{"set", "coder_mode", "{}"}, {"delete", "coder_mode"}} {
		err := run(args...)
		var fields interface{ JSONErrorFields() map[string]any }
		if clicore.ExitCodeFor(err) != 3 || !errors.As(err, &fields) || fields.JSONErrorFields()["code"] != "operator_only" ||
			!strings.Contains(err.Error(), "operator check is not wired") {
			t.Errorf("config %v without an operator check: %v", args, err)
		}
	}
	if err := run("set", "memory.runs", "off"); err != nil {
		t.Errorf("a row that isn't reserved: %v", err)
	}
}
