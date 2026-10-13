package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Sub-workflow calls (core.execute_workflow). A parent node starts child
// executions through the SubWorkflowRunner the engine puts on the run's
// context, pauses without holding its concurrency slot, and is woken when
// the last child reaches a terminal status (engine_subworkflow.go).
const (
	// TriggerTypeExecuteWorkflow is the trigger node type a child workflow
	// starts from, and the trigger type of every child execution.
	TriggerTypeExecuteWorkflow = "trigger.execute_workflow"

	// SubWorkflowItemsKey is the reserved trigger-data key holding the items
	// a child execution starts with; buildTriggerItems expands it into one
	// trigger item per entry.
	SubWorkflowItemsKey = "_items"

	// SubWorkflowParentKey is the reserved trigger-data key recording the
	// calling execution and the chain depth.
	SubWorkflowParentKey = "_subworkflow"

	// MaxSubWorkflowDepth stops a chain of sub-workflow calls (a workflow
	// calling itself, or a cycle of them) from running forever.
	MaxSubWorkflowDepth = 32
)

// SubWorkflowStart describes one child execution to start.
type SubWorkflowStart struct {
	ParentExecutionID string
	ParentNodeID      string
	// Workflow is the child workflow's ID or its name within the parent's
	// profile.
	Workflow string
	Items    []Item
	// ItemIndex is the parent item this child runs for, or -1 when one
	// child runs all items.
	ItemIndex int
	// Wait records that the parent waits for this child, so the child's end
	// wakes the parent.
	Wait bool
}

// SubWorkflowChild is a child execution's state as the parent sees it.
type SubWorkflowChild struct {
	ExecutionID string
	ItemIndex   int
	Status      string
	Error       string
	// Items are the main items of the child's last successful node, filled
	// once the child has finished.
	Items []Item
}

// Done reports whether the child reached a terminal status.
func (c SubWorkflowChild) Done() bool { return isTerminalStatus(c.Status) }

// Failed reports whether the child ended without completing its run.
func (c SubWorkflowChild) Failed() bool { return c.Status == "FAILED" || c.Status == "CANCELLED" }

// SubWorkflowRunner starts child executions and reports on them.
type SubWorkflowRunner interface {
	// StartSubWorkflow persists and enqueues a child execution and returns
	// its ID.
	StartSubWorkflow(ctx context.Context, req SubWorkflowStart) (string, error)
	// SubWorkflowChildren lists the children a parent node started, ordered
	// by item index.
	SubWorkflowChildren(ctx context.Context, parentExecutionID, parentNodeID string) ([]SubWorkflowChild, error)
}

type subWorkflowRunnerKey struct{}

// WithSubWorkflowRunner attaches r to ctx.
func WithSubWorkflowRunner(ctx context.Context, r SubWorkflowRunner) context.Context {
	return context.WithValue(ctx, subWorkflowRunnerKey{}, r)
}

// SubWorkflowRunnerFrom returns the runner WithSubWorkflowRunner attached.
func SubWorkflowRunnerFrom(ctx context.Context) (SubWorkflowRunner, bool) {
	r, ok := ctx.Value(subWorkflowRunnerKey{}).(SubWorkflowRunner)
	return r, ok && r != nil
}

func isTerminalStatus(status string) bool {
	switch status {
	case "SUCCESS", "SUCCESS_WITH_ERRORS", "FAILED", "CANCELLED":
		return true
	}
	return false
}

// subWorkflowTriggerItems expands the reserved SubWorkflowItemsKey entry of
// triggerData into items; ok is false when the key is absent.
func subWorkflowTriggerItems(triggerData map[string]interface{}) (items []Item, ok bool) {
	raw, ok := triggerData[SubWorkflowItemsKey].([]interface{})
	if !ok {
		return nil, false
	}
	items = make([]Item, 0, len(raw))
	for _, r := range raw {
		m, _ := r.(map[string]interface{})
		if m == nil {
			m = map[string]interface{}{}
		}
		items = append(items, NewItem(m))
	}
	return items, true
}

// subWorkflowDepth is the chain depth recorded in a run's trigger data (0
// for a run no sub-workflow call started).
func subWorkflowDepth(triggerData map[string]interface{}) int {
	parent, _ := triggerData[SubWorkflowParentKey].(map[string]interface{})
	switch d := parent["depth"].(type) {
	case float64:
		return int(d)
	case int:
		return d
	}
	return 0
}

// UnfinishedSubWorkflowChildren lists the child executions parentID started
// that have not reached a terminal status.
func UnfinishedSubWorkflowChildren(ctx context.Context, db *sql.DB, parentID string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT r.child_execution_id FROM subworkflow_runs r
		 JOIN workflow_executions x ON x.id = r.child_execution_id
		 WHERE r.parent_execution_id = ? AND x.status IN ('QUEUED','RUNNING','WAITING')`, parentID)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return nil, nil
		}
		return nil, fmt.Errorf("sub-workflow: list unfinished children: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("sub-workflow: list unfinished children: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// NudgeSubWorkflowParent makes the parent waiting on childID due for resume
// at once when childID was the last unfinished child of its node, so any
// engine's resume tick picks it up. It is the wake for a process with no
// engine (`workflow cancel`); an engine wakes the parent itself.
func NudgeSubWorkflowParent(ctx context.Context, db *sql.DB, childID string) error {
	var parentID, nodeID string
	err := db.QueryRowContext(ctx,
		`SELECT parent_execution_id, parent_node_id FROM subworkflow_runs WHERE child_execution_id = ? AND wait = 1`,
		childID).Scan(&parentID, &nodeID)
	if errors.Is(err, sql.ErrNoRows) || (err != nil && strings.Contains(err.Error(), "no such table")) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sub-workflow: parent lookup: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE workflow_executions SET resume_after = NULL
		 WHERE id = ? AND status = 'WAITING' AND NOT EXISTS (
		     SELECT 1 FROM subworkflow_runs r JOIN workflow_executions c ON c.id = r.child_execution_id
		     WHERE r.parent_execution_id = ? AND r.parent_node_id = ? AND r.wait = 1
		       AND c.status IN ('QUEUED','RUNNING','WAITING'))`,
		parentID, parentID, nodeID); err != nil {
		return fmt.Errorf("sub-workflow: wake parent: %w", err)
	}
	return nil
}
