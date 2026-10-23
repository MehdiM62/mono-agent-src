package main

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/aitaskkinds"
)

// wireAitaskKinds gives workflow-generate its existence check: the workflow must be in the task's
// profile under the same ownership rule as `workflow import --replace` (workflowio.OwnedWorkflow).
// A database that cannot be opened refuses the result.
func wireAitaskKinds(cfg *globalConfig) {
	aitaskkinds.WorkflowExists = func(ctx context.Context, profile, id string) (bool, error) {
		db, err := initDB(cfg)
		if err != nil {
			return false, fmt.Errorf("open database: %w", err)
		}
		defer db.Close()
		return ownedWorkflow(ctx, openWorkflowStore(db), db.DB, profile, id) != nil, nil
	}
}
