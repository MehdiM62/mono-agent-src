package orggrant

import (
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// core.evaluation records, scores and routes without writing anywhere;
// setOutputs (the default) writes into the dataset row.
func TestEvaluationClassifiedByOperation(t *testing.T) {
	for op, outbound := range map[string]bool{
		"": true, "setOutputs": true, "setInputs": false, "setMetrics": false, "checkIfEvaluating": false,
	} {
		cfg := map[string]interface{}{"operation": op}
		if got := isOutboundNode(workflow.WorkflowNode{Type: "core.evaluation", Config: cfg}); got != outbound {
			t.Errorf("%q: outbound = %v, want %v", op, got, outbound)
		}
	}
}
