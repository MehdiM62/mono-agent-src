package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// The auto-repair example is a valid workflow that starts at an error
// trigger and only uses config keys its node schemas define.
func TestErrorAutoRepairTemplate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "error-auto-repair.json"))
	if err != nil {
		t.Fatal(err)
	}
	wf, err := parseWorkflowFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateForActivation(wf); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if wf.Nodes[0].Type != TriggerNodeTypeError {
		t.Fatalf("first node = %s, want %s", wf.Nodes[0].Type, TriggerNodeTypeError)
	}
	for _, n := range wf.Nodes {
		schema, err := LoadDefaultSchema(n.Type)
		if err != nil {
			t.Fatalf("%s: no schema: %v", n.Type, err)
		}
		keys := map[string]bool{}
		for _, f := range schema.Fields {
			keys[f.Key] = true
		}
		for k := range n.Config {
			if !keys[k] {
				t.Errorf("%s (%s): config key %q is not in its schema", n.ID, n.Type, k)
			}
		}
	}
}

// An error trigger's payload expressions resolve against the payload the
// engine builds.
func TestErrorTriggerPayloadExpressions(t *testing.T) {
	failed := &WorkflowExecution{ID: "e1", TriggerType: "trigger.schedule", ErrorClass: string(ClassAuth),
		Nodes: []WorkflowExecutionNode{{NodeID: "n", NodeName: "Call", Status: "FAILED"}}}
	wf := &Workflow{ID: "w1", Name: "Daily", Nodes: []WorkflowNode{{ID: "n", Type: "http.request"}}}
	p := errorTriggerPayload(failed, wf, "HTTP 401")
	out, err := NewExpressionEngine().EvaluateString(
		"{{ $json.workflow.name }}|{{ $json.execution.error.message }}|{{ $json.execution.error.node.type }}|{{ $json.execution.mode }}|{{ $json.execution.error_class }}",
		ExpressionContext{JSON: p})
	if err != nil {
		t.Fatal(err)
	}
	if out != "Daily|HTTP 401|http.request|trigger|auth" {
		t.Fatalf("resolved %q", out)
	}
}
