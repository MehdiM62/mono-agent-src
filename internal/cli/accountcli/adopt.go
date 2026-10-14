package accountcli

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/cli/accountgate"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/storage"
)

// AdoptionSetting is the settings row that says a database has had its one try (spec D23).
const AdoptionSetting = "account_adoption"

// Adopt exchanges an older library login for the machine session: library.AdoptIntoAccount, which
// the CLI hands over so that a test can see whether the wiring calls it without a monoes.me to call.
type Adopt func(ctx context.Context, db *sql.DB, g *account.Guard) (bool, error)

// AdoptFirstRun is the first run of release R (spec section 8 step 3, D23): a library login that
// already exists, on any profile, is exchanged for the machine session. It runs before a gated or
// serving command (an open command calls monoes.me only when asked to) and does nothing while the
// gate is dormant (D22), with no guard installed, when somebody has signed in, or when the database
// is not there yet (a command must not create it for this).
//
// It tries once per database, ever: the try is claimed before it is made, with one row inserted only
// if absent, so two processes that start together make one try between them, and a failed try is
// never repeated (presenting a spent refresh token again could end every login of the account, A24).
// The exchange itself runs under the account store lock (library.AdoptIntoAccount). Any failure is
// swallowed: the command goes on, and nothing is printed or logged, tokens least of all. dbPath is
// the command's --db-path; adopt is library.AdoptIntoAccount.
func AdoptFirstRun(cmd *cobra.Command, dbPath string, adopt Adopt) {
	if accountgate.CommandClass(cmd) == accountgate.ClassOpen || account.EnforceDate().IsZero() {
		return
	}
	g := account.Current()
	if g == nil {
		return
	}
	if st := g.Status(); st.State != account.StateLocked || st.Reason != account.ReasonNotLoggedIn {
		return
	}
	path := clicore.ExpandPath(dbPath)
	if _, err := os.Stat(path); err != nil {
		return
	}
	db, err := storage.NewDatabase(path)
	if err != nil {
		return
	}
	defer db.Close()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	// Read first: a database that has had its try must not take the write lock on every command.
	var had string
	switch err := db.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, AdoptionSetting).Scan(&had); {
	case err == nil:
		return
	case !errors.Is(err, sql.ErrNoRows):
		return
	}
	claim, err := db.DB.ExecContext(ctx, `INSERT OR IGNORE INTO settings (key, value) VALUES (?, ?)`,
		AdoptionSetting, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return
	}
	if n, _ := claim.RowsAffected(); n == 1 { // otherwise this database has had its try, or another process is making it
		_, _ = adopt(ctx, db.DB, g)
	}
}

// AdoptBeforeGate runs the adoption ahead of the gate, which judges before cobra runs any command
// hook. The command and the database path are found on a throwaway tree, as the gate's AsksForHelp
// does, so that the real tree's flags are parsed only once, by its own run. A help request is an
// open invocation and is skipped. newProbe builds the CLI's tree with its classes applied;
// adoptFirstRun is AdoptFirstRun as the CLI calls it.
func AdoptBeforeGate(ctx context.Context, args []string, newProbe func() *cobra.Command, adoptFirstRun func(cmd *cobra.Command, dbPath string)) {
	probe := newProbe()
	probe.SetOut(io.Discard)
	probe.SetErr(io.Discard)
	cmd, flags, err := probe.Find(args)
	if err != nil || cmd == nil || cmd == probe {
		return
	}
	cmd.InitDefaultHelpFlag()
	if cmd.ParseFlags(flags) != nil {
		return
	}
	if help, _ := cmd.Flags().GetBool("help"); help {
		return
	}
	dbPath, err := cmd.Flags().GetString("db-path")
	if err != nil {
		return
	}
	cmd.SetContext(ctx)
	adoptFirstRun(cmd, dbPath)
}
