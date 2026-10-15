package workflow

import "testing"

func TestBuildExpressionContextTriggerAndVars(t *testing.T) {
	wf := &Workflow{ID: "wf1", Settings: map[string]interface{}{
		"variables": map[string]interface{}{"region": "eu"},
	}}
	exec := &WorkflowExecution{ID: "ex1", TriggerData: map[string]interface{}{"q": "go"}}
	ctx := buildExpressionContext(nil, nil, wf, exec)
	got, err := NewExpressionEngine().EvaluateString(
		`{{ $workflow.id }}/{{ $execution.id }}/{{ $trigger.q }}/{{ $vars.region }}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := "wf1/ex1/go/eu"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// No settings: $vars is empty, not an error.
	ctx = buildExpressionContext(nil, nil, &Workflow{ID: "wf1"}, exec)
	if got, _ := NewExpressionEngine().EvaluateString(`{{ $vars.region }}`, ctx); got != "" {
		t.Errorf("$vars without settings = %q", got)
	}
}
