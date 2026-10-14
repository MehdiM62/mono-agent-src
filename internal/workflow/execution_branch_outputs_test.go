package workflow_test

import (
	"context"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/nodes/control"
	"github.com/monoes/mono-agent/internal/workflow"
)

// branchNode splits its input like core.if: the first item on "true", the
// rest on "false".
type branchNode struct{}

func (branchNode) Type() string { return "test.branch" }
func (branchNode) Execute(_ context.Context, in workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	return []workflow.NodeOutput{
		{Handle: "true", Items: in.Items[:1]},
		{Handle: "false", Items: in.Items[1:]},
	}, nil
}

// seenNode records how many items $node[<name>] held when it ran.
type seenNode struct {
	mu   *sync.Mutex
	seen *[]int
	name string
}

func (seenNode) Type() string { return "test.seen" }
func (s seenNode) Execute(_ context.Context, in workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	*s.seen = append(*s.seen, len(in.NodeOutputs[s.name]))
	return []workflow.NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// QA-A U1: a node with only branch outputs (core.if, switch, a loop node)
// recorded no items, so $node["IF"] was empty downstream and the run's
// detail showed output_items: [].
func TestRunExecution_BranchOutputsAreRecorded(t *testing.T) {
	seen := &[]int{}
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.emit", func() workflow.NodeExecutor { return emitNode{n: 3} })
	reg.Register("test.branch", func() workflow.NodeExecutor { return branchNode{} })
	reg.Register("test.seen", func() workflow.NodeExecutor { return seenNode{mu: &sync.Mutex{}, seen: seen, name: "IF"} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
		{ID: "e", Type: "test.emit", Name: "Emit"},
		{ID: "b", Type: "test.branch", Name: "IF"},
		{ID: "s", Type: "test.seen", Name: "After"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "e"), conn("e", "main", "b"), conn("b", "false", "s"),
	}}
	store := &finishStore{}
	if err := runWF(t, reg, wf, store, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(*seen) != 1 || (*seen)[0] != 3 {
		t.Fatalf("$node[\"IF\"] item counts = %v, want [3] (true and false items)", *seen)
	}
	// Rows finish in order: Emit, IF, After.
	if len(store.finished) < 2 || len(store.finished[1].items) != 3 {
		t.Fatalf("IF's finished row = %+v, want its 3 routed items", store.finished)
	}
}

// A loop node records each batch once, not once for "loop" and again for
// the "batch" alias, and its "done" items on the last pass.
func TestRunExecution_LoopOutputsAreRecorded(t *testing.T) {
	seen := &[]int{}
	reg := workflow.NewNodeTypeRegistry()
	reg.Register(workflow.LoopNodeType, func() workflow.NodeExecutor { return &control.SplitInBatchesNode{} })
	reg.Register("test.emit", func() workflow.NodeExecutor { return emitNode{n: 5} })
	reg.Register("test.seen", func() workflow.NodeExecutor { return seenNode{mu: &sync.Mutex{}, seen: seen, name: "Loop"} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
		{ID: "e", Type: "test.emit", Name: "Emit"},
		{ID: "l", Type: workflow.LoopNodeType, Name: "Loop", Config: map[string]interface{}{"batch_size": 2}},
		{ID: "s", Type: "test.seen", Name: "Body"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "e"), conn("e", "main", "l"), conn("l", "loop", "s"), conn("s", "main", "l"),
	}}
	if err := runWF(t, reg, wf, &finishStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if want := []int{2, 2, 1}; !eqInts(*seen, want) {
		t.Fatalf("$node[\"Loop\"] item counts per pass = %v, want %v", *seen, want)
	}
}
