// Package workflowgenerate is `monoagentcli workflow generate`: a workflow built from a
// plain-language description by the user's AI agent, validated, and saved as an inactive
// draft. Everything the command needs from the CLI around it comes in through Deps, so it can
// be tested in isolation with a fake agent.
package workflowgenerate

import (
	"context"
	"database/sql"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/ai/generate"
	refcli "github.com/monoes/mono-agent/internal/cli/ref"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Deps is what the command takes from the CLI around it.
type Deps struct {
	// JSONOutput reports the global --json flag; it is read when the command runs.
	JSONOutput func() bool
	// ProfileID is the profile the command runs in once OpenDB has resolved it.
	ProfileID func() string
	// OpenDB opens (and migrates) the database the command is pointed at.
	OpenDB func() (*storage.Database, error)
	// NodeRegistry is the node types this install has (the catalog and the validation).
	NodeRegistry func(db *sql.DB) *workflow.NodeTypeRegistry
	// WorkflowStore is the profile's workflows, files and SQLite alike.
	WorkflowStore func(db *storage.Database) *workflow.HybridWorkflowStore
	// SaveDocument persists a workflow document for profileID, as `workflow save` does.
	SaveDocument func(ctx context.Context, store *workflow.HybridWorkflowStore, profileID string, doc workflow.Workflow) (*workflow.Workflow, error)
	// FindNodeDoc is the `ref node` entry of a node type, nil when there is none.
	FindNodeDoc func(typ string) *refcli.NodeDoc
	// ExecFor makes the model turn for runtime/model (a monomind agent exec turn).
	ExecFor func(ctx context.Context, runtime, model string) (generate.ExecFunc, error)
	// Scan is `agent scan`, for --runtime auto.
	Scan func(ctx context.Context) (*monomind.ScanResult, error)
}

// NewCommand is `workflow generate`.
func NewCommand(d Deps) *cobra.Command { return d.newCommand() }
