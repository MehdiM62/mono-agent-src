package workflow_test

import (
	"context"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// toolNode counts the times it runs; a guarded call must not run it.
type toolNode struct {
	mu   *sync.Mutex
	runs *int
}

func (toolNode) Type() string { return "test.tool" }
func (n toolNode) Execute(_ context.Context, in workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	n.mu.Lock()
	*n.runs++
	n.mu.Unlock()
	return []workflow.NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// callingRoot calls each of its ai_tool sub-nodes once and keeps what the
// call returns, as an AI agent does with a model's tool call.
type callingRoot struct {
	mu   *sync.Mutex
	got  *[]workflow.Item
	errs *[]error
}

func (callingRoot) Type() string { return "test.agent" }
func (r callingRoot) Execute(ctx context.Context, in workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	for _, s := range in.SubNodes {
		if s.Kind != "ai_tool" || s.Call == nil {
			continue
		}
		items, err := s.Call(ctx, map[string]interface{}{"to": "someone"})
		r.mu.Lock()
		*r.got = append(*r.got, items...)
		*r.errs = append(*r.errs, err)
		r.mu.Unlock()
	}
	return []workflow.NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// A tool an AI agent calls passes the exec guard as a step does: when the
// guard handles the call, the tool does not run and the model gets the
// guard's answer.
func TestAToolCallPassesTheExecGuard(t *testing.T) {
	var (
		mu       sync.Mutex
		runs     int
		got      []workflow.Item
		errs     []error
		guarded  []string
		seenTo   []interface{}
		heldItem = workflow.NewItem(map[string]interface{}{"held": true})
	)
	workflow.SetExecGuard(func(_ context.Context, nodeType string, _ workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		guarded = append(guarded, nodeType)
		if nodeType != "test.tool" {
			return nil, false, nil
		}
		seenTo = append(seenTo, config["to"])
		return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{heldItem}}}, true, nil
	})
	t.Cleanup(func() { workflow.SetExecGuard(nil) })

	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.emit", func() workflow.NodeExecutor { return emitNode{n: 1} })
	reg.Register("test.agent", func() workflow.NodeExecutor {
		return callingRoot{mu: &mu, got: &got, errs: &errs}
	})
	reg.Register("test.tool", func() workflow.NodeExecutor { return toolNode{mu: &mu, runs: &runs} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "a", Type: "test.agent", Name: "Agent"},
		{ID: "x", Type: "test.tool", Name: "Tool", Config: map[string]interface{}{"to": "{{ $json.to }}"}},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "a"),
		{SourceNodeID: "x", SourceHandle: "ai_tool", TargetNodeID: "a", TargetHandle: "ai_tool"},
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if runs != 0 {
		t.Errorf("the tool ran %d times; a call the guard handles must not run it", runs)
	}
	if len(seenTo) != 1 || seenTo[0] != "someone" {
		t.Errorf("the guard saw the tool's config %v; want it resolved against the call's arguments", seenTo)
	}
	if len(errs) != 1 || errs[0] != nil {
		t.Fatalf("tool call errors %v", errs)
	}
	if len(got) != 1 || got[0].JSON["held"] != true {
		t.Errorf("the agent got %+v; want the guard's answer", got)
	}
}

// A tool call the guard leaves alone runs as before.
func TestAToolCallTheGuardLeavesRuns(t *testing.T) {
	var (
		mu   sync.Mutex
		runs int
		got  []workflow.Item
		errs []error
	)
	workflow.SetExecGuard(func(context.Context, string, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, bool, error) {
		return nil, false, nil
	})
	t.Cleanup(func() { workflow.SetExecGuard(nil) })

	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.emit", func() workflow.NodeExecutor { return emitNode{n: 1} })
	reg.Register("test.agent", func() workflow.NodeExecutor {
		return callingRoot{mu: &mu, got: &got, errs: &errs}
	})
	reg.Register("test.tool", func() workflow.NodeExecutor { return toolNode{mu: &mu, runs: &runs} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "a", Type: "test.agent", Name: "Agent"},
		{ID: "x", Type: "test.tool", Name: "Tool"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "a"),
		{SourceNodeID: "x", SourceHandle: "ai_tool", TargetNodeID: "a", TargetHandle: "ai_tool"},
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if runs != 1 {
		t.Errorf("the tool ran %d times, want 1", runs)
	}
}
