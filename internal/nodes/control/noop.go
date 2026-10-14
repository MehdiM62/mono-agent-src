package control

import (
	"context"

	"github.com/monoes/mono-agent/internal/workflow"
)

// NoOpNode passes its input items through unchanged (n8n's No Operation
// node): a named point in a workflow that does nothing.
type NoOpNode struct{}

func (n *NoOpNode) Type() string { return "core.noop" }

func (n *NoOpNode) Execute(_ context.Context, input workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	return []workflow.NodeOutput{{Handle: "main", Items: input.Items}}, nil
}
