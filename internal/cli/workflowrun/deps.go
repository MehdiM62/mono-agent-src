// Package workflowrun is the part of `monoagentcli workflow` that runs workflows and bundled
// templates and shows or cancels their executions: `workflow run`, `workflow templates run`,
// `workflow execution` and `workflow cancel`. Everything the commands need from the CLI around
// them comes in through Deps, so they can be tested in isolation.
package workflowrun

import (
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Deps is what the commands take from the CLI around them.
type Deps struct {
	// JSONOutput reports the global --json flag; it is read when a command runs.
	JSONOutput func() bool
	// ProfileID is the profile the command runs in once OpenDB has resolved it; it is read when
	// a command runs.
	ProfileID func() string
	// OpenDB opens (and migrates) the database the command is pointed at.
	OpenDB func() (*storage.Database, error)
	// BuildEngine is the CLI's workflow engine (this profile only) and the cleanup to defer
	// with engine.Stop.
	BuildEngine func() (*workflow.WorkflowEngine, func(), error)
	// NodeTypes is the registry of the node types this install has.
	NodeTypes func() *workflow.NodeTypeRegistry
	// DryRun is `workflow run --dry-run`: validate and print the plan, start nothing.
	DryRun func(cmd *cobra.Command, workflowID string) error
}

// The subcommands, by the name main registers them under.

func (d Deps) NewRunCmd() *cobra.Command          { return d.newRunCmd() }
func (d Deps) NewTemplatesRunCmd() *cobra.Command { return d.newTemplatesRunCmd() }
func (d Deps) NewExecutionCmd() *cobra.Command    { return d.newExecutionCmd() }
func (d Deps) NewCancelCmd() *cobra.Command       { return d.newCancelCmd() }
