package main

import (
	"context"

	"github.com/monoes/mono-agent/internal/cli/orgenv"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/storage"
)

func raiseGrantTiersForWorkflow(ctx context.Context, db *storage.Database, load orggrant.WorkflowLoader, workflowID string) {
	orgenv.RaiseGrantTiersForWorkflow(ctx, db, load, workflowID)
}
