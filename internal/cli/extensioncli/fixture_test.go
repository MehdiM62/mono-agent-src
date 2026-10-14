package extensioncli

import (
	"database/sql"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testhome"
)

// The commands are tested here with the CLI around them faked through Deps. Each bridge test
// gets its own HOME and its own free port (MONOAGENT_EXTENSION_PORT, isolateBridgeEnv), so no
// test ever binds 9222 or 9232.

// Tests never touch the real ~/.monoagent or the OS keychain.
func TestMain(m *testing.M) {
	keyring.MockInit()
	testhome.Main(m)
}

// testDeps is the CLI around the commands; the database (for browsers/bind) is a temp one.
func testDeps(jsonOut bool) *Deps {
	return testDepsDB(jsonOut, "")
}

func testDepsDB(jsonOut bool, dbPath string) *Deps {
	return &Deps{
		JSONOutput: func() bool { return jsonOut },
		OpenDB: func() (*storage.Database, error) {
			db, err := storage.NewDatabase(dbPath)
			if err != nil {
				return nil, err
			}
			if err := db.ApplyMigrations(); err != nil {
				db.Close()
				return nil, err
			}
			return db, nil
		},
		ResolveProfileID: func(db *sql.DB, idOrName string) (string, error) {
			var id string
			if err := db.QueryRow(`SELECT id FROM profiles WHERE id = ? OR LOWER(name) = LOWER(?)`, idOrName, idOrName).Scan(&id); err != nil {
				return "", clicore.InvalidInput("profile %q not found (checked both id and name)", idOrName)
			}
			return id, nil
		},
		Version:        func() string { return "dev" }, // a development build, as `go test` builds are
		ServingCommand: func(c *cobra.Command) *cobra.Command { return c },
	}
}
