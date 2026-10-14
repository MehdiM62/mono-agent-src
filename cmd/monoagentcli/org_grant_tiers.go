package main

import (
	"context"

	"github.com/monoes/mono-agent/internal/cli/orgenv"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/storage"
)

// grantWorkflowLoader is orgenv.GrantWorkflowLoader on the CLI's workflow
// store.
func grantWorkflowLoader(db *storage.Database) orggrant.WorkflowLoader {
	return orgenv.GrantWorkflowLoader(db, openWorkflowStore)
}

func raiseGrantTiersForWorkflow(ctx context.Context, db *storage.Database, load orggrant.WorkflowLoader, workflowID string) {
	orgenv.RaiseGrantTiersForWorkflow(ctx, db, load, workflowID)
}
