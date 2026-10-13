package mcpnode

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestOwnServer lists the tools of monoagent's own MCP server through a
// built binary. Opt-in: set MCPNODE_MONOAGENTCLI to the binary and
// MCPNODE_HOME to a scratch HOME (the account gate may refuse there; the
// test then skips).
func TestOwnServer(t *testing.T) {
	bin, home := os.Getenv("MCPNODE_MONOAGENTCLI"), os.Getenv("MCPNODE_HOME")
	if bin == "" || home == "" {
		t.Skip("set MCPNODE_MONOAGENTCLI and MCPNODE_HOME to run")
	}
	got, err := run(t, context.Background(), nil, map[string]interface{}{
		"command":   bin,
		"args":      []interface{}{"mcp"},
		"env":       map[string]interface{}{"HOME": home, "BROWSER": "/bin/true"},
		"operation": "list_tools",
		"timeout":   float64(60),
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "account") || strings.Contains(err.Error(), "sign") {
			t.Skipf("account gate refused in the scratch HOME: %v", err)
		}
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("monoagent's MCP server listed no tools")
	}
	t.Logf("%d tools, first %v", len(got), got[0]["tool"])
}
