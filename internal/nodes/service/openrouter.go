package service

import (
	"context"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/workflow"
)

// OpenRouterNode is the retired service.openrouter node. It called an LLM
// over HTTP with its own API key, which mono-agent no longer does: AI runs
// through local agents via monomind. It stays registered so a saved
// workflow that still uses it fails with the migration hint from
// workflow.DeprecatedNodeTypes instead of "unknown node type". Its schema
// (internal/workflow/schemas/service.openrouter.json) is hand-written and
// empty: nothing it could be configured with is used.
type OpenRouterNode struct{}

func (n *OpenRouterNode) Type() string { return "service.openrouter" }

func (n *OpenRouterNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if extensionOwns("service.openrouter", config) {
		return apipkg.ExecuteExtension(ctx, "service.openrouter", input, config)
	}
	return nil, workflow.DeprecatedNodeError(n.Type())
}
