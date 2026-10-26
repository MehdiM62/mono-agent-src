package doctor

import (
	"context"
	"os"
	"path/filepath"

	"github.com/monoes/mono-agent/internal/health"
	"github.com/monoes/mono-agent/internal/workflow"
)

// addWorkflowPublicationHooks feeds the doctor's workflows.publications row. Reading never
// changes anything and never creates the workflow folder; the fix runs the same recovery every
// command that opens the workflows runs.
func addWorkflowPublicationHooks(env *health.Env) {
	open := func() (*workflow.HybridWorkflowStore, error) {
		dir := filepath.Join(env.DataDir, "workflows")
		if _, err := os.Stat(dir); err != nil {
			return nil, nil
		}
		files, err := workflow.NewWorkflowFileStore(dir)
		if err != nil {
			return nil, err
		}
		return workflow.NewHybridWorkflowStore(files, workflow.NewSQLiteWorkflowStore(env.DB)), nil
	}
	env.WorkflowPublications = func(ctx context.Context) (*health.WorkflowPublicationsInfo, error) {
		info := &health.WorkflowPublicationsInfo{}
		store, err := open()
		if err != nil || store == nil || env.DB == nil {
			return info, err
		}
		pubs, err := store.OpenPublications(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range pubs {
			info.Open = append(info.Open, health.OpenWorkflowPublication{WorkflowID: p.WorkflowID, Kind: p.Kind,
				Origin: p.Origin, State: p.State, Age: p.Age, OwnerGone: p.OwnerGone, Note: p.Note})
		}
		if info.Unreadable, info.Pending, err = store.FileDrift(ctx); err != nil {
			return info, err
		}
		info.ImportedInactive, err = store.ImportedInactive(ctx)
		return info, err
	}
	env.RecoverWorkflowPublications = func(ctx context.Context) (int, error) {
		store, err := open()
		if err != nil || store == nil || env.DB == nil {
			return 0, err
		}
		return store.RecoverPublications(ctx)
	}
	env.ResolveWorkflowPublication = func(ctx context.Context, id, keep string) error {
		store, err := open()
		if err != nil || store == nil || env.DB == nil {
			return err
		}
		return store.ResolveUnresolved(ctx, id, keep)
	}
}
