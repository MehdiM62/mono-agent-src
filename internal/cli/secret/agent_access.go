package secret

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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
	var (
		yes, anyHost bool
		names, hosts []string
		expires      string
	)
	cmd := &cobra.Command{
		Use:   "agent-access [on|off]",
		Short: "Let agent-written workflows read this profile's vault without your approval (off by default)",
		Long: "With no argument, shows whether it is on and its scope.\n" +
			"\n" +
			"By default a workflow the chat or an agent wrote or changed reads no @secret reference until\n" +
			"you save or activate it yourself. `on` lifts that for a run of such a workflow (a manual or\n" +
			"triggered run, and the stream of a trigger.sse node) and for nothing else: a reference that\n" +
			"came from run data or an expression, a single step an AI runs outside a workflow (node run),\n" +
			"and vault.secret_get stay refused, and the workflow's file and program limits stay.\n" +
			"\n" +
			"Scope: --secret NAME[,NAME...] allows only those secrets (default: all); --expires 24h ends it\n" +
			"after that long (default: never); --host HOST[,HOST...] allows a secret to be sent only to\n" +
			"those hosts by http.request, http.graphql, mcp.call (http) and trigger.sse. --host or --any-host\n" +
			"is required: --any-host lets any workflow an agent wrote send any allowed secret to any address\n" +
			"it names, so use it only for a profile whose secrets you accept an agent using.\n" +
			"\n" +
			"Every secret read this way is logged by name (never value): `agent-access audit`.\n" +
			"Only you can change it.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] != "on" && args[0] != "off" {
				return fmt.Errorf("agent-access takes on or off, not %q", args[0])
			}
			var grant secrets.AccessGrant
			if len(args) == 1 {
				if why := operator.Refusal(); why != "" {
					return clicore.OperatorOnly("agent-access decides whether an agent may use your vault, which is yours to choose: %s; run it yourself in your terminal or the app", why)
				}
				if args[0] == "on" {
					if !yes {
						return fmt.Errorf("pass --yes to confirm: the secrets named (every one by default) become usable by agent-written workflows")
					}
					var err error
					if grant, err = buildGrant(names, hosts, anyHost, expires); err != nil {
						return err
					}
				}
			}
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.DB.Close()
			// After OpenDB: only then is the profile resolved from a name to its id.
			profileID := d.ProfileID()
			if profileID == "" {
				return ErrProfileRequired
			}
			if len(args) == 1 {
				var err error
				if args[0] == "on" {
					err = secrets.SetAgentAccessGrant(cmd.Context(), db.DB, profileID, grant)
				} else {
					err = secrets.SetAgentAccess(cmd.Context(), db.DB, profileID, false)
				}
				if err != nil {
					return fmt.Errorf("saving agent access: %w", err)
				}
			}
			g, err := secrets.GetAgentAccess(cmd.Context(), db.DB, profileID)
			if err != nil {
				return fmt.Errorf("reading agent access: %w", err)
			}
			if d.JSONOutput() {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				out := map[string]any{"profile_id": profileID, "agent_access": g != nil}
				if g != nil {
					out["secrets"] = g.Secrets
					out["hosts"] = g.Hosts
					out["any_host"] = g.AnyHost
					out["expires_at"] = g.ExpiresAt
				}
				return enc.Encode(out)
			}
			w := cmd.OutOrStdout()
			if g == nil {
				fmt.Fprintln(w, "Off: a workflow an agent wrote reads no secret until you save or activate it.")
				return nil
			}
			fmt.Fprintln(w, "On: workflows written by an agent read this profile's vault without your approval.")
			fmt.Fprintf(w, "  secrets: %s\n", orAll(g.Secrets))
			if g.AnyHost {
				fmt.Fprintln(w, "  hosts:   ANY (a secret can be sent to any address a workflow names)")
			} else {
				fmt.Fprintf(w, "  hosts:   %s\n", strings.Join(g.Hosts, ", "))
			}
			if g.ExpiresAt != nil {
				fmt.Fprintf(w, "  expires: %s\n", g.ExpiresAt.Local().Format(time.RFC3339))
			} else {
				fmt.Fprintln(w, "  expires: never")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm turning it on")
	cmd.Flags().StringSliceVar(&names, "secret", nil, "Allow only these secrets (default: all of the profile's)")
	cmd.Flags().StringSliceVar(&hosts, "host", nil, "Send a secret only to these hosts")
	cmd.Flags().BoolVar(&anyHost, "any-host", false, "Do not bind secrets to hosts: a workflow can send an allowed secret to any address it names")
	cmd.Flags().StringVar(&expires, "expires", "", "Turn off after this long, e.g. 24h or 7d (default: never)")
	cmd.AddCommand(d.newSecretAgentAccessAuditCmd())
	return cmd
}

func orAll(names []string) string {
	if len(names) == 0 {
		return "all"
	}
	return strings.Join(names, ", ")
}

// buildGrant checks the flags of `agent-access on` and turns them into a grant.
func buildGrant(names, hosts []string, anyHost bool, expires string) (secrets.AccessGrant, error) {
	g := secrets.AccessGrant{AnyHost: anyHost}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			g.Secrets = append(g.Secrets, n)
		}
	}
	for _, h := range hosts {
		if h = strings.TrimSpace(h); h == "" {
			continue
		}
		if secrets.HostOf(h) == "" {
			return g, fmt.Errorf("--host %q is not a host name", h)
		}
		g.Hosts = append(g.Hosts, secrets.HostOf(h))
	}
	if len(g.Hosts) == 0 && !anyHost {
		return g, fmt.Errorf("pass --host HOST[,HOST...] to say where a secret may be sent, or --any-host to let a workflow an agent wrote send it to any address it names")
	}
	if anyHost && len(g.Hosts) > 0 {
		return g, fmt.Errorf("--host and --any-host exclude each other")
	}
	if expires != "" {
		dur, err := parseExpiry(expires)
		if err != nil {
			return g, err
		}
		t := time.Now().UTC().Add(dur)
		g.ExpiresAt = &t
	}
	return g, nil
}

func parseExpiry(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	} else if dur, err := time.ParseDuration(s); err == nil && dur > 0 {
		return dur, nil
	}
	return 0, fmt.Errorf("--expires %q: want a positive duration such as 24h or 7d", s)
}

// newSecretAgentAccessAuditCmd is `secret agent-access audit`: the secrets
// agent-written workflows read under agent access. Clearing it is the
// user's own, like the switch.
func (d Deps) newSecretAgentAccessAuditCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "List the secrets agent-written workflows read under agent access (names, never values)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if clear {
				if why := operator.Refusal(); why != "" {
					return clicore.OperatorOnly("clearing the agent-access audit erases the record of what an agent read, which is yours to decide: %s; run it yourself in your terminal or the app", why)
				}
			}
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("initializing database: %w", err)
			}
			defer db.DB.Close()
			profileID := d.ProfileID()
			if profileID == "" {
				return ErrProfileRequired
			}
			rows, err := secrets.AgentAccessAudit(cmd.Context(), db.DB, profileID)
			if err != nil {
				return fmt.Errorf("reading the audit: %w", err)
			}
			if rows == nil {
				rows = []secrets.AuditRow{}
			}
			w := cmd.OutOrStdout()
			if d.JSONOutput() {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				if err := enc.Encode(rows); err != nil {
					return err
				}
			} else if len(rows) == 0 {
				fmt.Fprintln(w, "No secret was read by an agent-written workflow.")
			} else {
				for _, r := range rows {
					fmt.Fprintf(w, "%s  secret %s  workflow %s  run %s  step %s\n", r.At, r.Secret, r.WorkflowID, r.ExecutionID, r.NodeName)
				}
			}
			if clear {
				n, err := secrets.ClearAgentAccessAudit(cmd.Context(), db.DB, profileID)
				if err != nil {
					return fmt.Errorf("clearing the audit: %w", err)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Cleared %d audit rows.\n", n)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&clear, "clear", false, "Print the audit, then delete it (yours to do, not an agent's)")
	return cmd
}
