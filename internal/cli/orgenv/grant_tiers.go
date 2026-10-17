package orgenv

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// GrantWorkflowLoader reads a granted workflow as it is now (file first,
// as execution does), for orggrant.RaiseTiers and the decision service. The
// store is opened with open on first use; the loader is safe for
// concurrent use.
func GrantWorkflowLoader(db *storage.Database, open func(*storage.Database) *workflow.HybridWorkflowStore) orggrant.WorkflowLoader {
	return GrantWorkflows(db, open).GetWorkflow
}

// GrantWorkflows reads workflows as execution does (files first): one by
// id, as GrantWorkflowLoader, and a profile's list, so a workflow a granted
// one calls by name is found as a run finds it. The store is opened with
// open on first use; it is safe for concurrent use.
func GrantWorkflows(db *storage.Database, open func(*storage.Database) *workflow.HybridWorkflowStore) *LazyWorkflows {
	return &LazyWorkflows{db: db, open: open}
}

// LazyWorkflows is GrantWorkflows' reader.
type LazyWorkflows struct {
	once  sync.Once
	db    *storage.Database
	open  func(*storage.Database) *workflow.HybridWorkflowStore
	store *workflow.HybridWorkflowStore
}

func (l *LazyWorkflows) get() *workflow.HybridWorkflowStore {
	l.once.Do(func() { l.store = l.open(l.db) })
	return l.store
}

// GetWorkflow reads one workflow, its file first.
func (l *LazyWorkflows) GetWorkflow(ctx context.Context, id string) (*workflow.Workflow, error) {
	return l.get().GetWorkflow(ctx, id)
}

// ListWorkflows lists a profile's workflows, files first.
func (l *LazyWorkflows) ListWorkflows(ctx context.Context, profileID string) ([]workflow.Workflow, error) {
	return l.get().ListWorkflows(ctx, profileID)
}

// RaiseGrantTiersForWorkflow runs after a workflow is saved: every grant of
// it whose stored tier the workflow has outgrown is raised (#284), each
// raise is noted on stderr, and the orgs involved have their files
// reconciled so the grant's display copy shows the new tier.
func RaiseGrantTiersForWorkflow(ctx context.Context, db *storage.Database, load orggrant.WorkflowLoader, workflowID string) {
	store := orggrant.NewStore(db.DB)
	grants, err := store.GrantsForWorkflow(ctx, workflowID)
	if err != nil || len(grants) == 0 {
		return
	}
	raised, err := store.RaiseTiers(ctx, grants, load)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: workflow %s: re-deriving grant tiers: %v\n", workflowID, err)
	}
	type orgKey struct{ profile, org string }
	affected := map[orgKey]bool{}
	for _, r := range raised {
		fmt.Fprintf(os.Stderr, "notice: org %s: %s\n", r.Org, r)
		affected[orgKey{r.Profile, r.Org}] = true
	}
	for k := range affected {
		root := profiledir.Root(db.DB, k.profile)
		doc, err := orgdesign.Load(root, k.org)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: org %s: %v (the grant tier was raised; its JSON was not updated)\n", k.org, err)
			continue
		}
		opts := orggrant.GenOptions{ProfileID: k.profile, CLIPath: SelfExecutable(), APIAddr: APIAddr(db), Workflow: load}
		if _, err := SaveReconciled(ctx, db, k.profile, root, doc, opts); err != nil {
			fmt.Fprintf(os.Stderr, "warning: org %s: %v (the grant tier was raised; its JSON was not updated)\n", k.org, err)
		}
	}
}
