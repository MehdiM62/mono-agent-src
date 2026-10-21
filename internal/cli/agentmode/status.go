package agentmode

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/sandboxprobe"
)

func (d Deps) newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether this process looks sandboxed (advisory only)",
		Long: "Looks for the marks an agent-mode sandbox leaves: " + sandboxprobe.EnvMarker + " in the environment (the bwrap profile " +
			"sets it), a ~/.monoagent that cannot be listed, is empty or is a tmpfs mount, and macOS App Sandbox's " +
			"container id. It is advisory only: a process can hide these, and finding none does not mean the process is " +
			"unsandboxed. Never use it to decide what a process may be trusted with; the sandbox is the boundary, not this report.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir := ""
			if d.StateDir != nil {
				dir = d.StateDir()
			} else if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, ".monoagent")
			}
			r := sandboxprobe.Detect(sandboxprobe.Real(dir))
			if d.JSONOutput != nil && d.JSONOutput() {
				return d.WriteJSON(cmd.OutOrStdout(), r)
			}
			w := cmd.OutOrStdout()
			if r.Sandboxed {
				fmt.Fprintln(w, "This process looks sandboxed:")
				for _, s := range r.Signals {
					fmt.Fprintln(w, "  - "+s)
				}
			} else {
				fmt.Fprintln(w, "No sign of a sandbox.")
			}
			fmt.Fprintln(w, "("+r.Advisory+")")
			return nil
		},
	}
}
