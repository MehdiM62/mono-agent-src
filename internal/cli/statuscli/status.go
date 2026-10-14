// Package statuscli is `monoagentcli status`: the database's counts and the daemons the org
// features need. Everything the command needs from the CLI around it comes in through Deps.
package statuscli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/storage"
)

// Deps is what the command takes from the CLI around it.
type Deps struct {
	// JSONOutput reports the global --json flag; it is read when the command runs.
	JSONOutput func() bool
	// OpenDB opens (and migrates) the database the command is pointed at, resolving --profile.
	OpenDB func() (*storage.Database, error)
	// DBPath is the global --db-path flag.
	DBPath func() string
	// ProfileID is the profile in effect; read after OpenDB.
	ProfileID func() string
	// StatusDaemons reports the workflow daemon and the org daemon for a profile's root.
	StatusDaemons func(root string) map[string]interface{}
}

// NewCommand returns `status`.
func NewCommand(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show system status overview",
		Long:  "Displays database path, session count, action counts by state, people count, and config count.",
		Example: `  monoagentcli status
  monoagentcli status --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.Close()

			dbPath := clicore.ExpandPath(d.DBPath())
			profileID := d.ProfileID()

			// Session count.
			var sessionCount int
			err = db.DB.QueryRow("SELECT COUNT(*) FROM crawler_sessions WHERE profile_id = ?", profileID).Scan(&sessionCount)
			if err != nil {
				sessionCount = 0
			}

			// People count.
			var peopleCount int
			err = db.DB.QueryRow("SELECT COUNT(*) FROM people WHERE profile_id = ?", profileID).Scan(&peopleCount)
			if err != nil {
				peopleCount = 0
			}

			// Config count.
			var configCount int
			err = db.DB.QueryRow("SELECT COUNT(*) FROM configs").Scan(&configCount)
			if err != nil {
				configCount = 0
			}

			// Template count.
			var templateCount int
			err = db.DB.QueryRow("SELECT COUNT(*) FROM templates").Scan(&templateCount)
			if err != nil {
				templateCount = 0
			}

			// List count.
			var listCount int
			err = db.DB.QueryRow("SELECT COUNT(*) FROM social_lists WHERE profile_id = ?", profileID).Scan(&listCount)
			if err != nil {
				listCount = 0
			}

			if d.JSONOutput() {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				out := map[string]interface{}{
					"db_path":      dbPath,
					"sessions":     sessionCount,
					"people":       peopleCount,
					"configs":      configCount,
					"templates":    templateCount,
					"social_lists": listCount,
				}
				// Both long-running processes the org features need
				// (contracts §4): the workflow daemon and the org daemon.
				for k, v := range d.StatusDaemons(profiledir.Root(db.DB, profileID)) {
					out[k] = v
				}
				return enc.Encode(out)
			}

			table := clicore.NewPlainTable(os.Stdout, []string{"Metric", "Value"}, []tw.Align{tw.AlignRight, tw.AlignLeft})

			table.Append([]string{"Database Path", dbPath})
			table.Append([]string{"Sessions", fmt.Sprintf("%d", sessionCount)})
			table.Append([]string{"People", fmt.Sprintf("%d", peopleCount)})

			table.Append([]string{"Configs", fmt.Sprintf("%d", configCount)})
			table.Append([]string{"Templates", fmt.Sprintf("%d", templateCount)})
			table.Append([]string{"Social Lists", fmt.Sprintf("%d", listCount)})
			table.Render()

			return nil
		},
	}
}
