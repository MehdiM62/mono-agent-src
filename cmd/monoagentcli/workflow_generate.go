package main

import (
	"context"
	"database/sql"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/ai/generate"
	"github.com/monoes/mono-agent/internal/cli/workflowgenerate"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// `workflow generate` lives in internal/cli/workflowgenerate; this is its adapter: the CLI's
// globals and helpers handed over as workflowgenerate.Deps.

// generateExecFor makes the model turn for runtime/model: a monomind agent exec turn.
var generateExecFor = func(ctx context.Context, runtime, model string) (generate.ExecFunc, error) {
	bin, _, err := monomind.Ensure(ctx)
	if err != nil {
		return nil, monomind.MarkNotSetup(err)
	}
	return generate.MonomindExec(bin, runtime, model), nil
}

// generateScan is `agent scan` for runtime "auto".
var generateScan = monomind.Scan

func workflowGenerateDeps(cfg *globalConfig) workflowgenerate.Deps {
	return workflowgenerate.Deps{
		JSONOutput: func() bool { return cfg.JSONOutput },
		ProfileID:  func() string { return cfg.ProfileID },
		OpenDB:     func() (*storage.Database, error) { return initDB(cfg) },
		NodeRegistry: func(db *sql.DB) *workflow.NodeTypeRegistry {
			return buildNodeRegistry(false, db)
		},
		WorkflowStore: openWorkflowStore,
		SaveDocument:  saveWorkflowDocument,
		FindNodeDoc:   findNodeDoc,
		ExecFor: func(ctx context.Context, runtime, model string) (generate.ExecFunc, error) {
			return generateExecFor(ctx, runtime, model)
		},
		Scan: func(ctx context.Context) (*monomind.ScanResult, error) { return generateScan(ctx) },
	}
}

func newWorkflowGenerateCmd(cfg *globalConfig) *cobra.Command {
	return workflowgenerate.NewCommand(workflowGenerateDeps(cfg))
}
