package workflow_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// rootNode records the sub-nodes the engine hands it.
type rootNode struct {
	mu   *sync.Mutex
	subs *[]workflow.SubNode
}

func (rootNode) Type() string { return "test.root" }
func (r rootNode) Execute(_ context.Context, in workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	r.mu.Lock()
	*r.subs = append(*r.subs, in.SubNodes...)
	r.mu.Unlock()
	return []workflow.NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// failNode must never run: sub-nodes are used by their root, not executed.
type failNode struct{}

func (failNode) Type() string { return "test.sub" }
func (failNode) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	panic("a sub-node ran as a step")
}

func TestRootGetsResolvedSubNodes(t *testing.T) {
	var subs []workflow.SubNode
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.emit", func() workflow.NodeExecutor { return emitNode{n: 1} })
	reg.Register("test.root", func() workflow.NodeExecutor { return rootNode{mu: &sync.Mutex{}, subs: &subs} })
	reg.Register("test.sub", func() workflow.NodeExecutor { return failNode{} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "r", Type: "test.root", Name: "Root"},
		{ID: "m", Type: "test.sub", Name: "Model", Config: map[string]interface{}{"model": "m-{{ $json.i }}"}},
		{ID: "x", Type: "test.sub", Name: "Off", Disabled: true},
		{ID: "e", Type: "test.sub", Name: "Inner", Config: map[string]interface{}{"k": "v"}},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "r"),
		{SourceNodeID: "m", SourceHandle: "ai_languageModel", TargetNodeID: "r", TargetHandle: "ai_languageModel"},
		{SourceNodeID: "x", SourceHandle: "ai_memory", TargetNodeID: "r", TargetHandle: "ai_memory"},
		{SourceNodeID: "e", SourceHandle: "ai_embedding", TargetNodeID: "m", TargetHandle: "ai_embedding"},
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("root got %d sub-nodes, want 1 (the disabled one left out): %+v", len(subs), subs)
	}
	m := subs[0]
	if m.Kind != "ai_languageModel" || m.Name != "Model" || m.Config["model"] != "m-0" {
		t.Errorf("sub-node %+v (config resolved against the root's item)", m)
	}
	if len(m.SubNodes) != 1 || m.SubNodes[0].Kind != "ai_embedding" || m.SubNodes[0].Config["k"] != "v" {
		t.Errorf("nested sub-node %+v", m.SubNodes)
	}
}

// A root that runs once per item gets its sub-nodes resolved against each
// item, not the first one.
func TestRootGetsSubNodesPerItem(t *testing.T) {
	var subs []workflow.SubNode
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.emit", func() workflow.NodeExecutor { return emitNode{n: 3} })
	reg.Register("test.root", func() workflow.NodeExecutor { return rootNode{mu: &sync.Mutex{}, subs: &subs} })
	reg.Register("test.sub", func() workflow.NodeExecutor { return failNode{} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "r", Type: "test.root", Name: "Root", Config: map[string]interface{}{"execute_once_per_item": true}},
		{ID: "m", Type: "test.sub", Name: "Model", Config: map[string]interface{}{"model": "m-{{ $json.i }}"}},
		{ID: "e", Type: "test.sub", Name: "Inner", Config: map[string]interface{}{"k": "k-{{ $json.i }}"}},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "r"),
		{SourceNodeID: "m", SourceHandle: "ai_languageModel", TargetNodeID: "r", TargetHandle: "ai_languageModel"},
		{SourceNodeID: "e", SourceHandle: "ai_embedding", TargetNodeID: "m", TargetHandle: "ai_embedding"},
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("root got %d sub-nodes over its runs, want 3 (one per item): %+v", len(subs), subs)
	}
	for i, s := range subs {
		want := fmt.Sprintf("m-%d", i)
		if s.Config["model"] != want || len(s.SubNodes) != 1 || s.SubNodes[0].Config["k"] != fmt.Sprintf("k-%d", i) {
			t.Errorf("run %d: sub-node %+v, want model %q and nested k-%d", i, s, want, i)
		}
	}
}

// A node whose schema declares sub_output is a sub-node even with nothing
// attached: it never runs as a step.
func TestSubOutputNodeNeverRuns(t *testing.T) {
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.sub", func() workflow.NodeExecutor { return failNode{} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "s", Type: "test.sub", Name: "Loose", Schema: &workflow.NodeSchema{SubOutput: "ai_tool"}},
	}}
	store := &recStore{}
	if err := runWF(t, reg, wf, store, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if st := store.statuses("s"); len(st) != 0 {
		t.Errorf("sub_output node has rows %v, want none", st)
	}
}

// perItemSub holds "session" back like ai.memory's session key.
type perItemSub struct{ failNode }

func (perItemSub) PerItemConfigFields() []string { return []string{"session"} }

// A sub-node's per-item fields stay raw for the root to resolve per item.
func TestSubNodePerItemFieldsStayRaw(t *testing.T) {
	var subs []workflow.SubNode
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.emit", func() workflow.NodeExecutor { return emitNode{n: 2} })
	reg.Register("test.root", func() workflow.NodeExecutor { return rootNode{mu: &sync.Mutex{}, subs: &subs} })
	reg.Register("test.sub", func() workflow.NodeExecutor { return perItemSub{} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "r", Type: "test.root", Name: "Root"},
		{ID: "m", Type: "test.sub", Name: "Mem", Config: map[string]interface{}{"session": "s-{{ $json.i }}", "other": "o-{{ $json.i }}"}},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "r"),
		{SourceNodeID: "m", SourceHandle: "ai_memory", TargetNodeID: "r", TargetHandle: "ai_memory"},
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(subs) != 1 || subs[0].Config["session"] != "s-{{ $json.i }}" || subs[0].Config["other"] != "o-0" {
		t.Fatalf("sub-node config %+v", subs)
	}
}
