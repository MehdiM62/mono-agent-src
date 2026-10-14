package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/integrations"
	"github.com/monoes/mono-agent/internal/health"
	"github.com/monoes/mono-agent/internal/mcp"
)

// `init`, `mcp`, `integration`, monoagent's skills and the MCP registration with the
// coding-agent runtimes live in internal/cli/integrations; this is their adapter: the CLI's
// globals handed over as integrations.Deps, and the old names doctor, crawl and root use.

func integrationsDeps(cfg *globalConfig) integrations.Deps {
	return integrations.Deps{
		JSONOutput: func() bool { return cfg.JSONOutput },
		ProfileID:  func() string { return cfg.ProfileID },
		DBPath:     func() string { return cfg.DBPath },
		Version:    version,
		Serving:    func(c *cobra.Command) *cobra.Command { return servingCommand(c) },
		RunMCP:     func(o mcp.Options) error { return runMCP(o) },
	}
}

func newInitCmd() *cobra.Command { return integrations.NewInitCommand() }

func newIntegrationCmd(cfg *globalConfig) *cobra.Command {
	return integrationsDeps(cfg).NewIntegrationCommand()
}

func newMCPCmd(cfg *globalConfig) *cobra.Command { return integrationsDeps(cfg).NewMCPCommand() }

// runMCP serves the MCP server on stdin and stdout. A test replaces it to see what the command
// would have started the server with.
var runMCP = mcp.Run

// claudeInitMarker is the first-run marker under ~/.monoagent.
const claudeInitMarker = integrations.ClaudeInitMarker

// claudeSkillNames are the skills distributed with monoagent.
var claudeSkillNames = integrations.ClaudeSkillNames

func claudeSkillFile(skillsDir, name string) string {
	return integrations.ClaudeSkillFile(skillsDir, name)
}

func installClaudeSkill(verbose bool) error { return integrations.InstallClaudeSkill(verbose) }

func runClaudeFirstRunCheck() { integrations.RunClaudeFirstRunCheck() }

func claudeSkillsState() (claudeFound bool, missing, stale []string) {
	return integrations.ClaudeSkillsState()
}

func registerAgentsMCP(ctx context.Context, progress func(string)) error {
	return integrations.RegisterAgentsMCP(ctx, progress)
}

func agentsMCPRegistration() (found, registered bool, where string) {
	return integrations.AgentsMCPRegistration()
}

func agentsMCPRuntimes() []health.MCPRuntime { return integrations.AgentsMCPRuntimes() }
