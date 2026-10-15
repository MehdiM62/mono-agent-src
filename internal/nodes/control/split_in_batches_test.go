package control

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func numItems(n int) []workflow.Item {
	out := make([]workflow.Item, n)
	for i := range out {
		out[i] = workflow.NewItem(map[string]interface{}{"i": i})
	}
	return out
}

func handleItems(outs []workflow.NodeOutput, handle string) (items []workflow.Item, present bool) {
	for _, o := range outs {
		if o.Handle == handle {
			return o.Items, true
		}
	}
	return nil, false
}

// One call per batch; the items fed back on continuation calls become the
// processed set emitted once on "done".
func TestSplitInBatches_IteratesWithState(t *testing.T) {
	n := &SplitInBatchesNode{}
	ctx := workflow.WithNodeState(context.Background(), nil)
	cfg := map[string]interface{}{"batch_size": 2}

	var batches []int
	in := numItems(5)
	for call := 0; call < 10; call++ {
		outs, err := n.Execute(ctx, workflow.NodeInput{Items: in}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if done, ok := handleItems(outs, "done"); ok {
			if len(done) != 5 {
				t.Errorf("done items = %d, want 5", len(done))
			}
			if workflow.NodeStateFromContext(ctx) != nil {
				t.Error("state should be cleared after done")
			}
			if !eqIntSlice(batches, []int{2, 2, 1}) {
				t.Errorf("batches = %v, want [2 2 1]", batches)
			}
			return
		}
		loop, _ := handleItems(outs, "loop")
		legacy, _ := handleItems(outs, "batch")
		if len(loop) != len(legacy) {
			t.Errorf("loop and batch outputs differ: %d vs %d", len(loop), len(legacy))
		}
		batches = append(batches, len(loop))
		in = loop // the body passes the batch back
	}
	t.Fatal("done never emitted")
}

func TestSplitInBatches_ResetStartsOver(t *testing.T) {
	n := &SplitInBatchesNode{}
	ctx := workflow.WithNodeState(context.Background(), nil)
	if _, err := n.Execute(ctx, workflow.NodeInput{Items: numItems(5)}, map[string]interface{}{"batch_size": 2}); err != nil {
		t.Fatal(err)
	}
	outs, err := n.Execute(ctx, workflow.NodeInput{Items: numItems(3)}, map[string]interface{}{
		"batch_size": 2, "options": map[string]interface{}{"reset": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	loop, _ := handleItems(outs, "loop")
	if len(loop) != 2 || loop[0].JSON["i"] != 0 {
		t.Errorf("reset should start a new set from the input, got %v", loop)
	}
}

func TestSplitInBatches_BatchSizeAliasAndValidation(t *testing.T) {
	n := &SplitInBatchesNode{}
	ctx := workflow.WithNodeState(context.Background(), nil)
	outs, err := n.Execute(ctx, workflow.NodeInput{Items: numItems(3)}, map[string]interface{}{"batchSize": float64(3)})
	if err != nil {
		t.Fatal(err)
	}
	if loop, _ := handleItems(outs, "loop"); len(loop) != 3 {
		t.Errorf("batchSize alias: loop = %d items, want 3", len(loop))
	}
	if _, err := n.Execute(ctx, workflow.NodeInput{Items: numItems(1)}, map[string]interface{}{}); err == nil {
		t.Error("missing batch_size should fail")
	}
}

func eqIntSlice(a, b []int) bool {
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
