package mcp

import (
	"context"

	"github.com/monoes/mono-agent/internal/cli/workflowio"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// grantRuntime is the grant-mode runtime: database and workflow store only.
// The profile comes from the grant row; --profile is checked against it in grantScope.
func grantRuntime(db *storage.Database, opts Options) (*runtime, error) {
	wfDir := opts.WorkflowsDir
	if wfDir == "" {
		wfDir = "~/.monoagent/workflows"
	}
	fileStore, err := workflow.NewWorkflowFileStore(expandHome(wfDir))
	if err != nil {
		fileStore = nil
	} else {
		_ = workflowio.StampFileWorkflows(context.Background(), db.DB, expandHome(wfDir))
	}
	return &runtime{
		db:    db,
		store: workflow.NewHybridWorkflowStore(fileStore, workflow.NewSQLiteWorkflowStore(db.DB)),
	}, nil
}
