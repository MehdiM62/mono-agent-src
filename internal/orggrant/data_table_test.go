package orggrant

import (
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// core.data_table reads, inserts and creates in the profile's own tables;
// updates, deletes, renames and clears count as outbound.
func TestDataTableClassifiedByOperation(t *testing.T) {
	cases := []struct {
		resource, op string
		outbound     bool
	}{
		{"", "", false}, {"row", "insert", false}, {"row", "get", false},
		{"row", "rowExists", false}, {"", "rowNotExists", false},
		{"row", "update", true}, {"row", "upsert", true}, {"row", "deleteRows", true},
		{"table", "list", false}, {"table", "", false},
		{"table", "create", false}, {"table", "update", true}, {"table", "clear", true}, {"table", "delete", true},
	}
	for _, tc := range cases {
		cfg := map[string]interface{}{"resource": tc.resource, "operation": tc.op}
		if got := isOutboundNode(workflow.WorkflowNode{Type: "core.data_table", Config: cfg}); got != tc.outbound {
			t.Errorf("%s/%s: outbound = %v, want %v", tc.resource, tc.op, got, tc.outbound)
		}
	}
}
