package workflow

import "context"

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
