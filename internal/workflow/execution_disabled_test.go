package workflow_test

import (
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func disabledWF(settings map[string]interface{}) *workflow.Workflow {
	return &workflow.Workflow{ID: "wf", Settings: settings, Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
		{ID: "i", Type: "test.items", Name: "Items"},
		{ID: "d", Type: "test.app", Name: "Off", Disabled: true},
		{ID: "r", Type: "test.rec", Name: "After"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "i"), conn("i", "main", "d"), conn("d", "main", "r"),
	}}
}

func TestDisabledNode_DefaultEmitsNothing(t *testing.T) {
	app, after, err := perItemRun(t, threeNames, newCallRec("test.app"), disabledWF(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(*app.calls) != 0 || len(*after.calls) != 0 {
		t.Fatalf("disabled node ran %d times, successor %d times; want 0 and 0", len(*app.calls), len(*after.calls))
	}
}

func TestDisabledNode_Passthrough(t *testing.T) {
	app, after, err := perItemRun(t, threeNames, newCallRec("test.app"),
		disabledWF(map[string]interface{}{workflow.DisabledNodesSetting: "passthrough"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(*app.calls) != 0 {
		t.Fatalf("disabled node ran %d times", len(*app.calls))
	}
	if len(*after.items) != 3 || (*after.items)[1].JSON["n"] != "b" {
		t.Fatalf("successor got %v, want the 3 input items", *after.items)
	}
}
