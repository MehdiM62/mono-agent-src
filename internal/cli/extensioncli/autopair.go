package extensioncli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	capturecli "github.com/monoes/mono-agent/internal/cli/capture"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// autoPairSetting is the settings row that turns the zero-step pairing off
// (`monoagentcli extension autopair off`).
const autoPairSetting = "extension.autoPair"

func autoPairValueOn(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "off", "no":
		return false
	}
	return true
}

// extensionAutoPair reads the kill-switch each time a browser asks to pair. It
// opens the database as the other bridge sinks do, and anything it cannot read
// leaves automatic pairing on: the default is on, and a locked database must
// not turn the feature off silently.
func extensionAutoPair(path string) func() bool {
	return func() bool {
		db, err := capturecli.OpenProfileDB(path)
		if err != nil {
			return true
		}
		defer db.Close()
		var v string
		if err := db.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, autoPairSetting).Scan(&v); err != nil {
			return true
		}
		return autoPairValueOn(v)
	}
}

func newExtensionAutoPairCmd(cfg *Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "autopair [on|off]",
		Short: "Show or set whether a signed-in machine pairs new browsers automatically",
		Long: "With automatic pairing on (the default), a browser with the MonoAgent extension that finds the\n" +
			"bridge running and this machine signed in receives its own pairing token without any step, and\n" +
			"can be listed and revoked with `extension browsers list|revoke`. Off, every browser needs\n" +
			"`extension pair`. The bridge reads the setting on every request, so no restart is needed. " + clicore.OperatorOnlyHelp,
		Example: "  monoagentcli extension autopair\n  monoagentcli extension autopair off",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] != "on" && args[0] != "off" {
				return clicore.InvalidInput("want on or off, got %q", args[0])
			}
			if len(args) == 1 {
				if err := cfg.operatorOnly("extension autopair decides whether new browsers can pair without a step"); err != nil {
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
			on := true
			if v, err := db.GetSetting(autoPairSetting); err == nil {
				on = autoPairValueOn(v)
			}
			if cfg.JSONOutput() {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]bool{"auto_pair": on})
			}
			if on {
				fmt.Fprintln(cmd.OutOrStdout(), "Automatic pairing is on: browsers pair themselves while you are signed in.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Automatic pairing is off: pair each browser with `monoagentcli extension pair`.")
			}
			return nil
		},
	}
}
