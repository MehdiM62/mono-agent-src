package data

import (
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// itemEvaluator resolves config values that a node declared per-item (see
// workflow.PerItemConfigResolver): the engine hands those over unresolved,
// and the node evaluates them once per input item against that item's $json.
type itemEvaluator struct {
	engine *workflow.ExpressionEngine
	input  workflow.NodeInput
}

func newItemEvaluator(input workflow.NodeInput) itemEvaluator {
	return itemEvaluator{engine: workflow.NewExpressionEngine(), input: input}
}

// value returns raw evaluated for item when it is a template string, raw
// unchanged otherwise (already resolved, or a plain literal).
func (e itemEvaluator) value(raw interface{}, item workflow.Item) (interface{}, error) {
	s, ok := raw.(string)
	if !ok || !strings.Contains(s, "{{") {
		return raw, nil
	}
	return e.engine.EvaluateValue(s, workflow.ExpressionContext{
		JSON:        item.JSON,
		Node:        e.input.NodeOutputs,
		WorkflowID:  e.input.WorkflowID,
		ExecutionID: e.input.ExecutionID,
	})
}
