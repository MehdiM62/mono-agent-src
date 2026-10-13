package workflow_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/nodes/control"
	"github.com/monoes/mono-agent/internal/workflow"
)

// recStore is fakeStore plus the execution-node rows and resume state a run
// writes, so loop tests can assert what ran and resume from a pause.
type recStore struct {
	fakeStore
	mu      sync.Mutex
	rows    []workflow.WorkflowExecutionNode
	waiting string
}

func (s *recStore) CreateExecutionNode(_ context.Context, en *workflow.WorkflowExecutionNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, *en)
	return nil
}

func (s *recStore) SetExecutionWaiting(_ context.Context, _ string, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waiting = state
	return nil
}

func (s *recStore) statuses(nodeID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.rows {
		if r.NodeID == nodeID {
			out = append(out, r.Status)
		}
	}
	return out
}

// emitNode ignores its input and emits n items {"i": 0..n-1}.
type emitNode struct{ n int }

func (emitNode) Type() string { return "test.emit" }
func (e emitNode) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	items := make([]workflow.Item, e.n)
	for i := range items {
		items[i] = workflow.NewItem(map[string]interface{}{"i": i})
	}
	return []workflow.NodeOutput{{Handle: "main", Items: items}}, nil
}

// recNode records the size of each input batch and marks every item seen.
type recNode struct {
	mu    *sync.Mutex
	calls *[]int
	items *[]workflow.Item
}

func newRec() recNode {
	return recNode{mu: &sync.Mutex{}, calls: &[]int{}, items: &[]workflow.Item{}}
}

func (recNode) Type() string { return "test.rec" }
func (r recNode) Execute(_ context.Context, in workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.calls = append(*r.calls, len(in.Items))
	out := make([]workflow.Item, len(in.Items))
	for i, it := range in.Items {
		js := map[string]interface{}{"seen": true}
		for k, v := range it.JSON {
			js[k] = v
		}
		out[i] = workflow.NewItem(js)
	}
	*r.items = append(*r.items, out...)
	return []workflow.NodeOutput{{Handle: "main", Items: out}}, nil
}

func (r recNode) snapshot() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), *r.calls...)
}

func runWF(t *testing.T, reg *workflow.NodeTypeRegistry, wf *workflow.Workflow, store workflow.WorkflowStore, resume string) error {
	t.Helper()
	dag, err := workflow.BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
	exec := &workflow.WorkflowExecution{ID: "e", WorkflowID: wf.ID, TriggerData: map[string]interface{}{}, ResumeState: resume}
	return workflow.RunExecution(context.Background(), exec, wf, dag, reg, store, nil, workflow.NewExpressionEngine(), zerolog.Nop())
}

func conn(src, handle, dst string) workflow.WorkflowConnection {
	return workflow.WorkflowConnection{SourceNodeID: src, SourceHandle: handle, TargetNodeID: dst, TargetHandle: "main"}
}

func loopRegistry(n int, body, done recNode) *workflow.NodeTypeRegistry {
	reg := workflow.NewNodeTypeRegistry()
	reg.Register(workflow.LoopNodeType, func() workflow.NodeExecutor { return &control.SplitInBatchesNode{} })
	reg.Register("test.emit", func() workflow.NodeExecutor { return emitNode{n: n} })
	reg.Register("test.rec", func() workflow.NodeExecutor { return body })
	reg.Register("test.done", func() workflow.NodeExecutor { return doneRec{done} })
	return reg
}

// doneRec is recNode under another type name, for the done branch.
type doneRec struct{ recNode }

func (doneRec) Type() string { return "test.done" }

func eqInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Port of the research repro: 5 items with batch_size 2 used to run the body
// once with all 5 items and never fire "done". The legacy "batch" handle
// with no back-edge now iterates per batch and fires done once.
func TestLoop_LegacyBatchHandleNoBackEdge(t *testing.T) {
	body, done := newRec(), newRec()
	reg := loopRegistry(5, body, done)
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "s", Type: workflow.LoopNodeType, Name: "S", Config: map[string]interface{}{"batch_size": 2}},
		{ID: "b", Type: "test.rec", Name: "B"},
		{ID: "d", Type: "test.done", Name: "D"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "s"),
		conn("s", "batch", "b"), conn("s", "done", "d"),
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := body.snapshot(); !eqInts(got, []int{2, 2, 1}) {
		t.Errorf("body calls = %v, want [2 2 1]", got)
	}
	if got := done.snapshot(); !eqInts(got, []int{5}) {
		t.Errorf("done calls = %v, want [5]", got)
	}
}

func backEdgeWorkflow(batch int) *workflow.Workflow {
	return &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "s", Type: workflow.LoopNodeType, Name: "S", Config: map[string]interface{}{"batch_size": batch}},
		{ID: "b", Type: "test.rec", Name: "B"},
		{ID: "d", Type: "test.done", Name: "D"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "s"),
		conn("s", "loop", "b"), conn("b", "main", "s"),
		conn("s", "done", "d"),
	}}
}

// n8n "Loop Over Items": the body runs once per batch, its output feeds the
// loop node back, and done receives every processed item once.
func TestLoop_BackEdgeLoopOverItems(t *testing.T) {
	body, done := newRec(), newRec()
	reg := loopRegistry(5, body, done)
	store := &recStore{}
	if err := runWF(t, reg, backEdgeWorkflow(2), store, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := body.snapshot(); !eqInts(got, []int{2, 2, 1}) {
		t.Errorf("body calls = %v, want [2 2 1]", got)
	}
	if got := done.snapshot(); !eqInts(got, []int{5}) {
		t.Fatalf("done calls = %v, want [5]", got)
	}
	// done gets the body's output (processed items), not the raw input.
	for _, it := range (*done.items)[:5] {
		if it.JSON["seen"] != true {
			t.Errorf("done item %v did not pass through the body", it.JSON)
		}
	}
	if st := store.statuses("d"); len(st) != 1 {
		t.Errorf("done node rows = %v, want exactly one run", st)
	}
}

// A body node that pauses mid-loop resumes in the same iteration; loop
// state survives the pause.
func TestLoop_PauseResumeMidLoop(t *testing.T) {
	body, done := newRec(), newRec()
	reg := loopRegistry(5, body, done)
	pauser := &pauseOnce{at: 2}
	reg.Register("test.pause", func() workflow.NodeExecutor { return pauser })
	wf := backEdgeWorkflow(2)
	wf.Nodes = append(wf.Nodes, workflow.WorkflowNode{ID: "p", Type: "test.pause", Name: "P"})
	wf.Connections = []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "s"),
		conn("s", "loop", "b"), conn("b", "main", "p"), conn("p", "main", "s"),
		conn("s", "done", "d"),
	}
	store := &recStore{}
	err := runWF(t, reg, wf, store, "")
	if !errors.Is(err, workflow.ErrExecutionPaused) {
		t.Fatalf("first run err = %v, want ErrExecutionPaused", err)
	}
	if store.waiting == "" {
		t.Fatal("no resume state persisted")
	}
	if err := runWF(t, reg, wf, store, store.waiting); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := body.snapshot(); !eqInts(got, []int{2, 2, 1}) {
		t.Errorf("body calls = %v, want [2 2 1]", got)
	}
	if got := done.snapshot(); !eqInts(got, []int{5}) {
		t.Errorf("done calls = %v, want [5]", got)
	}
}

// pauseOnce pauses on its at-th call, then passes items through.
type pauseOnce struct {
	mu    sync.Mutex
	calls int
	at    int
}

func (*pauseOnce) Type() string { return "test.pause" }
func (p *pauseOnce) Execute(_ context.Context, in workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls == p.at {
		return nil, workflow.ErrNodePaused
	}
	return []workflow.NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// Nested loops: the inner loop restarts for every outer batch.
func TestLoop_Nested(t *testing.T) {
	inner, outerDone := newRec(), newRec()
	reg := loopRegistry(4, inner, outerDone)
	reg.Register("test.y", func() workflow.NodeExecutor { return passNode{} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "s1", Type: workflow.LoopNodeType, Name: "S1", Config: map[string]interface{}{"batch_size": 2}},
		{ID: "s2", Type: workflow.LoopNodeType, Name: "S2", Config: map[string]interface{}{"batch_size": 1}},
		{ID: "x", Type: "test.rec", Name: "X"},
		{ID: "y", Type: "test.y", Name: "Y"},
		{ID: "d", Type: "test.done", Name: "D"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "s1"),
		conn("s1", "loop", "s2"),
		conn("s2", "loop", "x"), conn("x", "main", "s2"),
		conn("s2", "done", "y"), conn("y", "main", "s1"),
		conn("s1", "done", "d"),
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := inner.snapshot(); !eqInts(got, []int{1, 1, 1, 1}) {
		t.Errorf("inner body calls = %v, want [1 1 1 1]", got)
	}
	if got := outerDone.snapshot(); !eqInts(got, []int{4}) {
		t.Errorf("outer done calls = %v, want [4]", got)
	}
}

type passNode struct{}

func (passNode) Type() string { return "test.y" }
func (passNode) Execute(_ context.Context, in workflow.NodeInput, _ map[string]interface{}) ([]workflow.NodeOutput, error) {
	return []workflow.NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// reset=true treats every call as a new set — the pagination pattern: the
// loop keeps going while the body returns items and ends when it returns
// none.
func TestLoop_ResetPagination(t *testing.T) {
	body, done := newRec(), newRec()
	reg := loopRegistry(1, body, done)
	pages := &pager{left: 3}
	reg.Register("test.page", func() workflow.NodeExecutor { return pages })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "s", Type: workflow.LoopNodeType, Name: "S", Config: map[string]interface{}{"batch_size": 10, "reset": true}},
		{ID: "p", Type: "test.page", Name: "P"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "s"), conn("s", "loop", "p"), conn("p", "main", "s"),
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if pages.calls != 4 {
		t.Errorf("page calls = %d, want 4 (3 pages + the empty one)", pages.calls)
	}
}

type pager struct{ left, calls int }

func (*pager) Type() string { return "test.page" }
func (p *pager) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	p.calls++
	if p.left == 0 {
		return []workflow.NodeOutput{{Handle: "main"}}, nil
	}
	p.left--
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{workflow.NewItem(map[string]interface{}{"page": p.left})}}}, nil
}

// The engine's iteration guard stops a loop node that never reports done.
func TestLoop_IterationGuard(t *testing.T) {
	reg := workflow.NewNodeTypeRegistry()
	reg.Register(workflow.LoopNodeType, func() workflow.NodeExecutor { return neverDone{} })
	reg.Register("test.y", func() workflow.NodeExecutor { return passNode{} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "s", Type: workflow.LoopNodeType, Name: "S"},
		{ID: "b", Type: "test.y", Name: "B"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "s"), conn("s", "loop", "b"), conn("b", "main", "s"),
	}}
	err := runWF(t, reg, wf, &recStore{}, "")
	if err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("err = %v, want a loop guard error", err)
	}
}

// neverDone always emits one fresh item on loop and never on done.
type neverDone struct{}

func (neverDone) Type() string { return workflow.LoopNodeType }
func (neverDone) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	return []workflow.NodeOutput{{Handle: "loop", Items: []workflow.Item{workflow.NewItem(map[string]interface{}{})}}}, nil
}

// A merge inside the loop body that also takes a branch from outside the
// loop runs on every iteration, waiting only for its in-loop input after the
// first (the outside branch does not run again).
func TestLoop_BodyMergeWithOutsideInput(t *testing.T) {
	merge, done := newRec(), newRec()
	reg := loopRegistry(3, merge, done)
	reg.Register("test.y", func() workflow.NodeExecutor { return passNode{} })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "f", Type: "test.emit", Name: "F"},
		{ID: "s", Type: workflow.LoopNodeType, Name: "S", Config: map[string]interface{}{"batch_size": 1}},
		{ID: "b", Type: "test.y", Name: "B"},
		{ID: "c", Type: "test.y", Name: "C"},
		{ID: "m", Type: "test.rec", Name: "M"},
		{ID: "d", Type: "test.done", Name: "D"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "f"), conn("f", "main", "s"), conn("t", "main", "c"),
		conn("s", "loop", "b"), conn("b", "main", "m"), conn("c", "main", "m"),
		conn("m", "main", "s"), conn("s", "done", "d"),
	}}
	if err := runWF(t, reg, wf, &recStore{}, ""); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := merge.snapshot(); !eqInts(got, []int{2, 1, 1}) {
		t.Errorf("merge calls = %v, want [2 1 1]", got)
	}
	if got := done.snapshot(); len(got) != 1 {
		t.Errorf("done calls = %v, want one", got)
	}
}
