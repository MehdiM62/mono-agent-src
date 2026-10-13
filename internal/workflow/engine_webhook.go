package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/monoes/mono-agent/internal/account"
)

// Reasons a webhook delivery could not start a run. The webhook server maps
// them to the status it answers with (webhookStartStatus), so a caller learns
// its request was dropped instead of getting a 200 for nothing.
var (
	ErrWebhookWorkflowNotFound = errors.New("workflow not found")
	ErrWebhookWorkflowInactive = errors.New("workflow is not active")
	ErrWebhookQueueFull        = errors.New("execution queue is full")
)

// wireWebhookRuns lets the webhook server start runs through
// handleWebhookTrigger (which reports the execution ID or why there is none),
// read executions back for the waiting response modes, and remember delivery
// IDs in the database.
func (e *WorkflowEngine) wireWebhookRuns() {
	e.webhookServer.SetRunner(e.handleWebhookTrigger, e.store)
	var db *sql.DB
	if e.store != nil {
		db = e.store.RawDB()
	}
	if db != nil {
		e.webhookServer.SetDeliveryStore(NewSQLWebhookDeliveries(db))
	} else {
		e.webhookServer.SetDeliveryStore(NewMemoryWebhookDeliveries())
	}
}

// handleWebhookTrigger is handleTrigger for a webhook delivery: it creates
// the execution record and queues it, and returns the execution ID or the
// reason no run was started. prepare (may be nil) is called with the new
// execution's ID before it is queued, so a response-mode waiter is in place
// before the run can reach a Respond to Webhook node.
func (e *WorkflowEngine) handleWebhookTrigger(workflowID, nodeID string, items []Item, prepare func(executionID string)) (string, error) {
	if err := account.Require(context.Background()); err != nil {
		if n, due := e.drops.note(); due {
			e.logger.Warn().Err(err).Int("dropped", n).Str("workflow_id", workflowID).
				Msg("engine: monoes.me login required; trigger dropped (logged once a minute)")
		}
		return "", err
	}
	ctx := e.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	wf, err := e.store.GetWorkflow(ctx, workflowID)
	if err != nil {
		return "", fmt.Errorf("load workflow: %w", err)
	}
	if wf == nil {
		return "", ErrWebhookWorkflowNotFound
	}
	if !wf.IsActive {
		return "", ErrWebhookWorkflowInactive
	}
	triggerType := "unknown"
	for _, n := range wf.Nodes {
		if n.ID == nodeID {
			triggerType = n.Type
			break
		}
	}

	triggerData := make(map[string]interface{})
	if len(items) > 0 && items[0].JSON != nil {
		triggerData = items[0].JSON
	}
	exec := &WorkflowExecution{
		WorkflowID:  workflowID,
		ProfileID:   executionProfileID(wf, e.profileID),
		Status:      "QUEUED",
		TriggerType: triggerType,
		// Persisted once the trigger_node_id column lands, so an adopted,
		// resumed or retried run fires only this trigger's branch.
		TriggerNodeID: nodeID,
		TriggerData:   triggerData,
	}
	if err := e.store.CreateExecution(ctx, exec); err != nil {
		return "", fmt.Errorf("create execution: %w", err)
	}
	if prepare != nil {
		prepare(exec.ID)
	}

	req := ExecutionRequest{
		WorkflowID:    workflowID,
		ExecutionID:   exec.ID,
		TriggerType:   triggerType,
		TriggerNodeID: nodeID,
		TriggerData:   triggerData,
	}
	if err := e.queue.Enqueue(req); err != nil {
		e.logger.Warn().Str("execution_id", exec.ID).Str("workflow_id", workflowID).
			Msg("engine: webhook trigger: queue full, execution will not run")
		e.persistExecutionFinished(e.logger, exec.ID, "FAILED", "queue full")
		return exec.ID, ErrWebhookQueueFull
	}
	return exec.ID, nil
}
