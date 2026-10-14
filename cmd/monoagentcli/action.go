package main

import (
	"strings"

	"github.com/spf13/cobra"

	actioncli "github.com/monoes/mono-agent/internal/cli/action"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// newActionCmd is `monoagentcli action`, which lives in internal/cli/action.
func newActionCmd(cfg *globalConfig) *cobra.Command {
	return actioncli.NewCommand(actioncli.Deps{
		Install:        autoinstallDeps(cfg),
		WithJSONErrors: func(cmd *cobra.Command) { withJSONErrors(cfg, cmd) },
	})
}

// truncateStr shortens s to at most n characters, appending "..." if truncated.
func truncateStr(s string, n int) string { return clicore.TruncateStr(s, n) }

// safeName reports whether s is usable as a path element: no separators,
// no "..", no leading dot.
func safeName(s string) bool {
	return s != "" && !strings.ContainsAny(s, `/\`) && !strings.Contains(s, "..") && !strings.HasPrefix(s, ".")
}
