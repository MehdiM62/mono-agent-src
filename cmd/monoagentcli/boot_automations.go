package main

import (
	"os"
	"testing"

	"github.com/spf13/cobra"

	automationcli "github.com/monoes/mono-agent/internal/cli/automation"
	"github.com/monoes/mono-agent/internal/nodes"
)

// skipAutomationBoot keeps test binaries from booting the real ~/.monoagent;
// a test that wants the registry sets HOME to a temp dir and clears it.
var skipAutomationBoot = testing.Testing()

// bootAutomationsFor boots the automation registry before cmd runs
// (automationcli.Boot), unless this is a test binary.
func bootAutomationsFor(cmd *cobra.Command) {
	if skipAutomationBoot {
		return
	}
	automationcli.Boot(cmd, func() error {
		_, err := nodes.BootAutomations("")
		return err
	}, os.Stderr)
}

// needsAutomations is automationcli.NeedsAutomations.
func needsAutomations(cmd *cobra.Command) bool { return automationcli.NeedsAutomations(cmd) }
