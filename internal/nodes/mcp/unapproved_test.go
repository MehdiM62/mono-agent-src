package mcpnode

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A workflow the user has not approved starts no stdio MCP server (a program
// on this computer); an approved one does (TestStdioListTools).
func TestNoStdioServerInAnUnapprovedWorkflow(t *testing.T) {
	ctx := workflow.WithAIAuthored(context.Background())
	if _, err := run(t, ctx, nil, stdioConfig(map[string]interface{}{"operation": "list_tools"})); !errors.Is(err, workflow.ErrUnapprovedProgram) {
		t.Fatalf("err = %v, want the unapproved-workflow refusal", err)
	}
}
