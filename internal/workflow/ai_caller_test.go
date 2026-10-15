package workflow

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

type aiCallerProbeNode struct {
	item   *map[string]interface{}
	caller *interface{}
}

func (aiCallerProbeNode) Type() string { return "test.aicallerprobe" }
func (n aiCallerProbeNode) Execute(ctx context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	*n.item = in.Items[0].JSON
	*n.caller = TriggerDataFrom(ctx)[AICallerKey]
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// The AI-caller mark reaches the run's nodes through its context, never
// through the trigger's item.
func TestAICallerMarkStaysOutOfItems(t *testing.T) {
	var item map[string]interface{}
	var caller interface{}
	reg := NewNodeTypeRegistry()
	reg.Register("test.aicallerprobe", func() NodeExecutor { return aiCallerProbeNode{item: &item, caller: &caller} })
	wf := &Workflow{
		ID: "wf-ai",
		Nodes: []WorkflowNode{
			{ID: "t", WorkflowID: "wf-ai", Type: "trigger.manual", Name: "T"},
			{ID: "p", WorkflowID: "wf-ai", Type: "test.aicallerprobe", Name: "Probe"},
		},
		Connections: []WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "p", TargetHandle: "main"}},
	}
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	exec := &WorkflowExecution{ID: "e-ai", WorkflowID: wf.ID, TriggerType: "trigger.manual", TriggerNodeID: "t",
		TriggerData: map[string]interface{}{"who": "zed", AICallerKey: "an agent (CLAUDECODE)"}}
	if err := RunExecution(context.Background(), exec, wf, dag, reg, &stubStore{}, nil, NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatalf("RunExecution: %v", err)
	}
	if _, leaked := item[AICallerKey]; leaked || item["who"] != "zed" {
		t.Errorf("trigger item = %v, want the input without %s", item, AICallerKey)
	}
	if caller != "an agent (CLAUDECODE)" {
		t.Errorf("the run's context lost the mark: %v", caller)
	}
}
