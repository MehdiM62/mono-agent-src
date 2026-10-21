package extensioncli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/extension"
)

// The browsers that paired themselves (the extension's zero-step claim, see
// internal/extension/autopair.go), each with a token of its own. `browsers`
// alone still lists the browsers connected right now.

type pairedRow struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Origin   string `json:"origin"`
	Agent    string `json:"agent,omitempty"`
	PairedAt string `json:"paired_at"`
}

func pairedRows(list []extension.PairedBrowser) []pairedRow {
	rows := make([]pairedRow, 0, len(list))
	for _, b := range list {
		rows = append(rows, pairedRow{ID: b.ID, Label: b.Label, Origin: b.Origin, Agent: b.Agent, PairedAt: b.PairedAt.Format(time.RFC3339)})
	}
	return rows
}

func newExtensionBrowsersListCmd(cfg *Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List the browsers that paired themselves automatically",
		Long:    "Every browser that signed itself in through the bridge (no `extension pair` step) has its own pairing token. Revoke one with `extension browsers revoke <id>`.",
		Example: "  monoagentcli extension browsers list\n  monoagentcli extension browsers list --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := extension.ListPairedBrowsers()
			if err != nil {
				return err
			}
			// Only how many wait, never who: a request's details are for the
			// approve/deny commands and the card, not for a listing.
			waiting, _ := extension.ListPendingClaims()
			return printPaired(cmd.OutOrStdout(), cfg.JSONOutput(), pairedRows(list), len(waiting))
		},
	}
}

func printPaired(out io.Writer, asJSON bool, rows []pairedRow, waiting int) error {
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"paired": rows, "pending": waiting})
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "No browser has paired itself automatically.")
	} else {
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tLABEL\tBROWSER\tPAIRED")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%q\t%q\t%s\n", r.ID, r.Label, r.Agent, r.PairedAt)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if waiting > 0 {
		fmt.Fprintf(out, "%d pairing request(s) waiting for approval: `monoagentcli extension approve` lists them; `extension deny --all` clears them.\n", waiting)
	}
	return nil
}

func newExtensionBrowsersRevokeCmd(cfg *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Cut one automatically paired browser off (its token stops working)",
		Long: "<id> is an id (or its first 4+ characters) from `extension browsers list`. The browser's\n" +
			"open connection is closed within about half a minute, and it cannot connect again until it\n" +
			"pairs again. Other browsers are not affected. " + clicore.OperatorOnlyHelp,
		Example: "  monoagentcli extension browsers revoke 3f2a9c",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cfg.operatorOnly("extension browsers revoke cuts a browser off from MonoAgent"); err != nil {
				return err
			}
			gone, err := extension.RevokePairedBrowser(args[0])
			if errors.Is(err, extension.ErrNoSuchBrowser) {
				return clicore.NotFound("%v — see `monoagentcli extension browsers list`", err)
			}
			if err != nil {
				return err
			}
			if cfg.JSONOutput() {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"revoked": pairedRows([]extension.PairedBrowser{gone})[0]})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked %s (%q). It is disconnected within about half a minute; to turn automatic pairing off for good: monoagentcli extension autopair off\n", gone.ID, gone.Label)
			return nil
		},
	}
}
