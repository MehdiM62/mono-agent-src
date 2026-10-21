package extensioncli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	capturecli "github.com/monoes/mono-agent/internal/cli/capture"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/extension"
)

// autoPairSetting is the settings row holding the pairing mode
// (`monoagentcli extension autopair ask|auto|off`).
const autoPairSetting = "extension.autoPair"

// extensionPairMode reads the mode each time a browser asks to pair. It opens
// the database as the other bridge sinks do, and anything it cannot read
// leaves the safe default, ask: a locked database must never loosen pairing.
func extensionPairMode(path string) func() string {
	return func() string {
		db, err := capturecli.OpenProfileDB(path)
		if err != nil {
			return extension.PairModeAsk
		}
		defer db.Close()
		var v string
		if err := db.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, autoPairSetting).Scan(&v); err != nil {
			return extension.PairModeAsk
		}
		return extension.ParsePairMode(v)
	}
}

// pairCards files pending claims as cards in the HIL queue of every profile
// (the claim is the machine's, and each profile's queue is read separately).
type pairCards struct {
	path     string
	profiles extension.ProfileSource
}

func newPairCards(path string, profiles extension.ProfileSource) extension.PairCards {
	return pairCards{path: path, profiles: profiles}
}

func (p pairCards) Open(ctx context.Context, c extension.PendingClaim) {
	db, err := capturecli.OpenProfileDB(p.path)
	if err != nil {
		return
	}
	defer db.Close()
	list, err := p.profiles(ctx)
	if err != nil {
		return
	}
	for _, pr := range list {
		_ = extension.OpenPairCard(ctx, db.DB, pr.ID, c)
	}
}

func (p pairCards) Settle(ctx context.Context, claimID, status string) {
	db, err := capturecli.OpenProfileDB(p.path)
	if err != nil {
		return
	}
	defer db.Close()
	_ = extension.SettlePairCards(ctx, db.DB, claimID, status)
}

func newExtensionAutoPairCmd(cfg *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "autopair [ask|auto|off]",
		Short: "Show or set how new browsers pair: ask (default), auto or off",
		Long: "How a browser with the MonoAgent extension gets its own pairing token:\n" +
			"  ask   (default) the browser's request waits for you. Approve it on its card in the app's\n" +
			"        Human in Loop queue or with `extension approve`; deny with `extension deny`.\n" +
			"  auto  no step: any extension (or local process, or other OS user on this machine) that asks\n" +
			"        while you are signed in gets a token. Only for a machine only you use.\n" +
			"  off   no claims; pair each browser with `extension pair`.\n" +
			"The bridge reads the setting on every request, so no restart is needed. " + clicore.OperatorOnlyHelp,
		Example: "  monoagentcli extension autopair\n  monoagentcli extension autopair auto",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] != "ask" && args[0] != "auto" && args[0] != "off" {
				return clicore.InvalidInput("want ask, auto or off, got %q", args[0])
			}
			if len(args) == 1 {
				if err := cfg.operatorOnly("extension autopair decides who can pair a browser without a token"); err != nil {
					return err
				}
			}
			db, err := cfg.OpenDB()
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()
			if len(args) == 1 {
				if err := db.SetSetting(autoPairSetting, args[0]); err != nil {
					return err
				}
			}
			mode := extension.PairModeAsk
			if v, err := db.GetSetting(autoPairSetting); err == nil {
				mode = extension.ParsePairMode(v)
			}
			if cfg.JSONOutput() {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"pair_mode": mode})
			}
			switch mode {
			case extension.PairModeAuto:
				fmt.Fprintln(cmd.OutOrStdout(), "Pairing is automatic: any extension that asks while you are signed in gets a token. Use this only on a machine only you use.")
			case extension.PairModeOff:
				fmt.Fprintln(cmd.OutOrStdout(), "Pairing is off: pair each browser with `monoagentcli extension pair`.")
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "Pairing asks you: a new browser waits until you approve it (`monoagentcli extension approve --latest` or its card in the app).")
			}
			return nil
		},
	}
}

func newExtensionDecideCmd(cfg *Deps, approve bool) *cobra.Command {
	use, short := "deny [claim-id]", "Deny a browser's request to pair"
	if approve {
		use, short = "approve [claim-id]", "Approve a browser's request to pair"
	}
	var latest bool
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long: short + ". The request is named by its id (4+ characters; the card in the Human in Loop queue and the\n" +
			"bridge log show it) or --latest. Approving gives that browser a token that reaches your documents, vault,\n" +
			"tasks and page control: approve only a browser you just installed or opened. " + clicore.OperatorOnlyHelp,
		Example: "  monoagentcli extension approve --latest\n  monoagentcli extension deny 1a2b",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (len(args) == 1) == latest {
				return clicore.InvalidInput("give a claim id, or --latest")
			}
			verb := "approve"
			if !approve {
				verb = "deny"
			}
			if err := cfg.operatorOnly(verb + " a browser's request to pair"); err != nil {
				return err
			}
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			c, err := extension.DecidePendingClaim(id, approve)
			if err != nil {
				if errors.Is(err, extension.ErrNoSuchClaim) || errors.Is(err, extension.ErrClaimExpired) {
					return clicore.InvalidInput("%v (waiting requests: %s)", err, waitingClaims())
				}
				return err
			}
			if db, err := cfg.OpenDB(); err == nil {
				status := "rejected"
				if approve {
					status = "approved"
				}
				_ = extension.SettlePairCards(cmd.Context(), db.DB, c.ID, status)
				db.Close()
			}
			if cfg.JSONOutput() {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"claim": c.ID, "status": c.Status, "browser": c.Label, "origin": c.Origin})
			}
			if approve {
				fmt.Fprintf(cmd.OutOrStdout(), "Approved %s (%s, %s): its token is handed over the next time it asks.\n", c.ID, c.Label, c.Origin)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Denied %s (%s, %s).\n", c.ID, c.Label, c.Origin)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&latest, "latest", false, "decide the newest waiting request")
	return cmd
}

func waitingClaims() string {
	list, _ := extension.ListPendingClaims()
	if len(list) == 0 {
		return "none"
	}
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s left\n", c.ID, c.Label, c.Origin, time.Until(c.ExpiresAt).Round(time.Second))
	}
	_ = tw.Flush()
	return "\n" + b.String()
}
