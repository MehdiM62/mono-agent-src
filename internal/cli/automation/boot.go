package automation

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Boot boots the automation registry (legacy fold + DefSource) before any
// command runs, so every path that reads action definitions (node
// schema/run, workflow run/validate/inputs, action …, mcp, httpapi, daemon)
// sees the installed packages. Help, version and shell completion skip it;
// a bare command group boots too, since it can't be told apart from a
// runnable parent such as `daemon`. The fold only writes when a legacy
// directory changed; a failure leaves the legacy loader in place and is a
// warning on stderr. boot is nodes.BootAutomations, as the CLI hands it over.
func Boot(cmd *cobra.Command, boot func() error, stderr io.Writer) {
	if !NeedsAutomations(cmd) {
		return
	}
	if err := boot(); err != nil {
		fmt.Fprintf(stderr, "warning: automations: %v (using ~/.monoagent/actions only)\n", err)
	}
}

// NeedsAutomations reports whether cmd boots the automation registry: every
// command but help, version and shell completion (and what is below them).
func NeedsAutomations(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "help", "version", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
			return false
		}
	}
	return true
}
