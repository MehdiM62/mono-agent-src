package workflow

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
)

// versionedStore is the part of a store that knows workflow versions; both
// SQLiteWorkflowStore and HybridWorkflowStore implement it.
type versionedStore interface {
	ExecutionWorkflowVersion(ctx context.Context, executionID string) (int, error)
	GetWorkflowVersion(ctx context.Context, workflowID string, version int) (*WorkflowVersion, error)
}

// atomicSaver is a store that saves a whole definition in one step.
type atomicSaver interface {
	SaveWorkflow(ctx context.Context, w *Workflow) error
}

// loadExecutionWorkflow loads the workflow an execution runs: the definition
// of the version the execution was pinned to when it was created, so a
// WAITING run resumes against the graph it started with even if the workflow
// was edited since. Without a pinned version or its snapshot (an execution
// from before versions existed, a pruned version, a store without versions)
// it is the workflow's current definition. Not-found and load errors are the
// current workflow's, as before.
func (e *WorkflowEngine) loadExecutionWorkflow(ctx context.Context, log zerolog.Logger, req ExecutionRequest) (*Workflow, error) {
	wf, err := e.store.GetWorkflow(ctx, req.WorkflowID)
	if err != nil || wf == nil {
		return wf, err
	}
	vs, ok := e.store.(versionedStore)
	if !ok {
		return wf, nil
	}
	pinned, err := vs.ExecutionWorkflowVersion(ctx, req.ExecutionID)
	if err != nil || pinned == 0 {
		return wf, nil
	}
	ver, err := vs.GetWorkflowVersion(ctx, wf.ID, pinned)
	if err != nil || ver == nil || ver.Snapshot == nil {
		log.Warn().Err(err).Int("workflow_version", pinned).
			Msg("engine: pinned workflow version unavailable; running the current definition")
		return wf, nil
	}
	if current, err := definitionHash(wf); err == nil && current == ver.Hash {
		return wf, nil
	}
	log.Info().Int("workflow_version", pinned).
		Msg("engine: running the pinned workflow version (the workflow has changed since this execution was created)")
	out := ver.Snapshot.Apply(wf)
	out.Version = pinned
	return out, nil
}

// saveDefinition writes w's definition with the store's all-or-nothing save
// when it has one, and otherwise (a store without one, e.g. a test double)
// row, nodes and connections in turn.
func (e *WorkflowEngine) saveDefinition(ctx context.Context, w *Workflow) error {
	if s, ok := e.store.(atomicSaver); ok {
		return s.SaveWorkflow(ctx, w)
	}
	if err := e.store.UpdateWorkflow(ctx, w); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := e.store.SaveWorkflowNodes(ctx, w.ID, w.Nodes); err != nil {
		return fmt.Errorf("save nodes: %w", err)
	}
	if err := e.store.SaveWorkflowConnections(ctx, w.ID, w.Connections); err != nil {
		return fmt.Errorf("save connections: %w", err)
	}
	return nil
}

// rearmTriggers swaps an active workflow's registered triggers for those of
// its stored definition.
func (e *WorkflowEngine) rearmTriggers(ctx context.Context, id string) error {
	full, err := e.store.GetWorkflow(ctx, id)
	if err != nil {
		return err
	}
	if full == nil {
		return ErrWorkflowNotFound
	}
	e.triggerMgr.DeactivateWorkflow(id)
	if err := e.triggerMgr.ActivateWorkflow(ctx, full); err != nil {
		e.triggerMgr.DeactivateWorkflow(id)
		return err
	}
	return nil
}
