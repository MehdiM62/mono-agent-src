package workflow

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// overlapNode tracks how many instances run at once. Each call lingers so
// that concurrently scheduled siblings overlap.
type overlapNode struct {
	inflight *int32
	peak     *int32
}

func (overlapNode) Type() string { return "test.overlap" }
func (o overlapNode) Execute(_ context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	n := atomic.AddInt32(o.inflight, 1)
	for {
		p := atomic.LoadInt32(o.peak)
		if n <= p || atomic.CompareAndSwapInt32(o.peak, p, n) {
			break
		}
	}
	time.Sleep(50 * time.Millisecond)
	atomic.AddInt32(o.inflight, -1)
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

func fanOutWorkflow(settings map[string]interface{}) *Workflow {
	wf := &Workflow{ID: "wf-par", Settings: settings, Nodes: []WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "a", Type: "test.overlap", Name: "A"},
		{ID: "b", Type: "test.overlap", Name: "B"},
		{ID: "c", Type: "test.overlap", Name: "C"},
		{ID: "m", Type: "test.overlap", Name: "M"},
	}}
	for _, id := range []string{"a", "b", "c"} {
		wf.Connections = append(wf.Connections,
			WorkflowConnection{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: id},
			WorkflowConnection{SourceNodeID: id, SourceHandle: "main", TargetNodeID: "m"})
	}
	return wf
}

func runOverlap(t *testing.T, settings map[string]interface{}) (peak int32, store *stubStore) {
	t.Helper()
	var inflight int32
	reg := NewNodeTypeRegistry()
	reg.Register("test.overlap", func() NodeExecutor { return overlapNode{inflight: &inflight, peak: &peak} })
	wf := fanOutWorkflow(settings)
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	store = &stubStore{}
	exec := &WorkflowExecution{ID: "e", WorkflowID: wf.ID}
	if err := RunExecution(context.Background(), exec, wf, dag, reg, store, nil, NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatalf("run: %v", err)
	}
	return peak, store
}

func TestParallelBranches_DefaultSequential(t *testing.T) {
	peak, _ := runOverlap(t, nil)
	if peak != 1 {
		t.Errorf("peak concurrency = %d, want 1 without parallel_branches", peak)
	}
}

func TestParallelBranches_Enabled(t *testing.T) {
	peak, store := runOverlap(t, map[string]interface{}{"parallel_branches": true})
	if peak < 2 {
		t.Errorf("peak concurrency = %d, want >= 2 with parallel_branches", peak)
	}
	// The merge node still waits for all three branches.
	rec := store.nodeRecord("m")
	if rec == nil || len(rec.InputItems) != 3 {
		t.Fatalf("merge record = %+v, want 3 input items", rec)
	}
}

// Results are applied in topological order, so a merge sees its inputs in
// the same order whether branches ran in parallel or not.
func TestParallelBranches_DeterministicMergeOrder(t *testing.T) {
	reg := NewNodeTypeRegistry()
	var mu sync.Mutex
	var seen []interface{}
	reg.Register("test.tag", func() NodeExecutor { return tagNode{} })
	reg.Register("test.collect", func() NodeExecutor {
		return collectNode{fn: func(items []Item) {
			mu.Lock()
			defer mu.Unlock()
			for _, it := range items {
				seen = append(seen, it.JSON["tag"])
			}
		}}
	})
	wf := &Workflow{ID: "wf", Settings: map[string]interface{}{"parallel_branches": true}, Nodes: []WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "a", Type: "test.tag", Name: "A", Config: map[string]interface{}{"tag": "a", "sleep_ms": 60}},
		{ID: "b", Type: "test.tag", Name: "B", Config: map[string]interface{}{"tag": "b"}},
		{ID: "m", Type: "test.collect", Name: "M"},
	}, Connections: []WorkflowConnection{
		{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "a"},
		{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "b"},
		{SourceNodeID: "a", SourceHandle: "main", TargetNodeID: "m"},
		{SourceNodeID: "b", SourceHandle: "main", TargetNodeID: "m"},
	}}
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	order, _ := dag.TopologicalSort()
	var want []interface{}
	for _, n := range order {
		if n.ID == "a" || n.ID == "b" {
			want = append(want, n.Config["tag"])
		}
	}
	exec := &WorkflowExecution{ID: "e", WorkflowID: wf.ID}
	if err := RunExecution(context.Background(), exec, wf, dag, reg, &stubStore{}, nil, NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != want[0] || seen[1] != want[1] {
		t.Errorf("merge saw %v, want topological order %v", seen, want)
	}
}

type tagNode struct{}

func (tagNode) Type() string { return "test.tag" }
func (tagNode) Execute(_ context.Context, _ NodeInput, cfg map[string]interface{}) ([]NodeOutput, error) {
	switch ms := cfg["sleep_ms"].(type) {
	case int:
		time.Sleep(time.Duration(ms) * time.Millisecond)
	case float64:
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	return []NodeOutput{{Handle: "main", Items: []Item{NewItem(map[string]interface{}{"tag": cfg["tag"]})}}}, nil
}

type collectNode struct{ fn func([]Item) }

func (collectNode) Type() string { return "test.collect" }
func (c collectNode) Execute(_ context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	c.fn(in.Items)
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}
