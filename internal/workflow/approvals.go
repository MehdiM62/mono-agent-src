package workflow

import (
	"context"
	"strings"
)

// Operator approval of workflow definitions (#831 review C). A run of a
// definition the operator never approved — one the chat, an agent or a
// workflow made or changed since the operator last saved or activated the
// workflow from the app or their own terminal — is an AI run, whatever
// triggered it: RunExecution marks its context (AIAuthoredFrom), and what
// refuses AI runs (internal/operator.AIRun) refuses it. The approvals live in
// workflow_approvals (migration 106), outside the definition, so no import
// or settings key can claim one; the operator's commands record them
// (ApproveDefinition, internal/operator.ApproveWorkflow).

type aiAuthoredKey struct{}

// WithAIAuthored marks ctx as a run of a definition the operator never
// approved.
func WithAIAuthored(ctx context.Context) context.Context {
	return context.WithValue(ctx, aiAuthoredKey{}, true)
}

// AIAuthoredFrom reports whether ctx is a run of a definition the operator
// never approved.
func AIAuthoredFrom(ctx context.Context) bool {
	v, _ := ctx.Value(aiAuthoredKey{}).(bool)
	return v
}

// definitionApprover is a store that keeps the operator's approvals.
type definitionApprover interface {
	DefinitionApproved(ctx context.Context, workflowID, hash string) (bool, error)
}

// operatorApproved reports whether wf's definition is one the operator
// approved. A store that keeps no approvals approves everything; an error
// approves nothing.
func operatorApproved(ctx context.Context, store WorkflowStore, wf *Workflow) bool {
	a, ok := store.(definitionApprover)
	if !ok {
		return true
	}
	hash, err := definitionHash(wf)
	if err != nil {
		return false
	}
	approved, err := a.DefinitionApproved(ctx, wf.ID, hash)
	return err == nil && approved
}

// DefinitionApproved reports whether the operator approved definition hash
// of the workflow. A database from before approvals (no table: a test's
// schema) approves everything.
func (s *SQLiteWorkflowStore) DefinitionApproved(ctx context.Context, workflowID, hash string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_approvals WHERE workflow_id = ? AND definition_hash = ?`,
		workflowID, hash).Scan(&n)
	if err != nil && strings.Contains(err.Error(), "no such table") {
		return true, nil
	}
	return n > 0, err
}

// ApproveDefinition records the workflow's current definition as the
// operator's. Only the operator's own commands call it.
func (s *SQLiteWorkflowStore) ApproveDefinition(ctx context.Context, workflowID string) error {
	wf, err := s.GetWorkflow(ctx, workflowID)
	if err != nil || wf == nil {
		return err
	}
	return s.approve(ctx, wf)
}

func (s *SQLiteWorkflowStore) approve(ctx context.Context, wf *Workflow) error {
	hash, err := definitionHash(wf)
	if err != nil {
		return err
	}
	// A workflow with no SQLite row (a file nothing synced yet) can't run,
	// and its approval would have nothing to go with on delete.
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO workflow_approvals (workflow_id, definition_hash)
		SELECT ?, ? WHERE EXISTS (SELECT 1 FROM workflows WHERE id = ?)`, wf.ID, hash, wf.ID)
	return err
}

// DefinitionApproved is the SQLite store's: approvals live there.
func (h *HybridWorkflowStore) DefinitionApproved(ctx context.Context, workflowID, hash string) (bool, error) {
	return h.sql.DefinitionApproved(ctx, workflowID, hash)
}

// ApproveDefinition records the definition the engine would run (the file's,
// else SQLite's) as the operator's.
func (h *HybridWorkflowStore) ApproveDefinition(ctx context.Context, workflowID string) error {
	wf, err := h.GetWorkflow(ctx, workflowID)
	if err != nil || wf == nil {
		return err
	}
	return h.sql.approve(ctx, wf)
}

// ApproveDefinition records the workflow's current definition as the
// operator's, through the engine's store (when it keeps approvals).
func (e *WorkflowEngine) ApproveDefinition(ctx context.Context, workflowID string) error {
	if a, ok := e.store.(interface {
		ApproveDefinition(ctx context.Context, workflowID string) error
	}); ok {
		return a.ApproveDefinition(ctx, workflowID)
	}
	return nil
}
