package secret

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/operator"
	"github.com/monoes/mono-agent/internal/secrets"
)

// newSecretAgentAccessCmd is `secret agent-access [on|off]`: whether a
// workflow the user never approved (one the chat or an agent wrote or
// changed) may read this profile's vault, with no approval per workflow.
// Off by default. Turning it on or off is the user's own: an agent, the
// chat or a workflow is refused, and so is a process an agent started.
func (d Deps) newSecretAgentAccessCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "agent-access [on|off]",
		Short: "Let agent-written workflows read this profile's vault without your approval (off by default)",
		Long: "With no argument, shows whether it is on.\n" +
			"\n" +
			"By default a workflow the chat or an agent wrote or changed reads no @secret reference until\n" +
			"you save or activate it yourself. `on` lifts that for every secret in this profile: such a\n" +
			"workflow can send any of them to any address it names, so turn it on only for a profile whose\n" +
			"secrets you accept an agent using. A reference that came from run data or an expression, and a\n" +
			"single step an AI runs outside a workflow (node run), stay refused. Only you can change it.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			profileID := d.ProfileID()
			if profileID == "" {
				return ErrProfileRequired
			}
			if len(args) == 1 && args[0] != "on" && args[0] != "off" {
				return fmt.Errorf("agent-access takes on or off, not %q", args[0])
			}
			if len(args) == 1 {
				if why := operator.Refusal(); why != "" {
					return clicore.OperatorOnly("agent-access decides whether an agent may use your vault, which is yours to choose: %s; run it yourself in your terminal or the app", why)
				}
				if args[0] == "on" && !yes {
					return fmt.Errorf("pass --yes to confirm: every secret in this profile becomes usable by agent-written workflows")
				}
			}
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.DB.Close()
			if len(args) == 1 {
				if err := secrets.SetAgentAccess(cmd.Context(), db.DB, profileID, args[0] == "on"); err != nil {
					return fmt.Errorf("saving agent access: %w", err)
				}
			}
			on, err := secrets.AgentAccess(cmd.Context(), db.DB, profileID)
			if err != nil {
				return fmt.Errorf("reading agent access: %w", err)
			}
			if d.JSONOutput() {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"profile_id": profileID, "agent_access": on})
			}
			if on {
				fmt.Fprintln(cmd.OutOrStdout(), "On: workflows written by an agent read this profile's vault without your approval.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Off: a workflow an agent wrote reads no secret until you save or activate it.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm turning it on")
	return cmd
}
