package workflow_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// callRec records each call's resolved "q" and batch size, and fails a
// call whose first item has fail=true.
type callRec struct {
	typ   string
	mu    *sync.Mutex
	calls *[]string
	sizes *[]int
}

func newCallRec(typ string) callRec {
	return callRec{typ: typ, mu: &sync.Mutex{}, calls: &[]string{}, sizes: &[]int{}}
}

func (c callRec) Type() string { return c.typ }
func (c callRec) Execute(_ context.Context, in workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	q, _ := config["q"].(string)
	*c.calls = append(*c.calls, q)
	*c.sizes = append(*c.sizes, len(in.Items))
	if f, _ := in.Items[0].JSON["fail"].(bool); f {
		return nil, errors.New("boom")
	}
	out := workflow.NewItem(map[string]interface{}{"q": q})
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{out}}}, nil
}

// itemsNode emits fixed items.
type itemsNode []map[string]interface{}

func (itemsNode) Type() string { return "test.items" }
func (n itemsNode) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	items := make([]workflow.Item, len(n))
	for i, j := range n {
		items[i] = workflow.NewItem(j)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: items}}, nil
}

func perItemWF(settings map[string]interface{}, cfg map[string]interface{}, typ string) *workflow.Workflow {
	return &workflow.Workflow{ID: "wf", Settings: settings, Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
		{ID: "i", Type: "test.items", Name: "Items"},
		{ID: "a", Type: typ, Name: "App", Config: cfg},
		{ID: "r", Type: "test.rec", Name: "After"},
	}, Connections: []workflow.WorkflowConnection{
		conn("t", "main", "i"), conn("i", "main", "a"), conn("a", "main", "r"),
	}}
}

func perItemRun(t *testing.T, items itemsNode, app callRec, wf *workflow.Workflow) (callRec, recNode, error) {
	t.Helper()
	after := newRec()
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.items", func() workflow.NodeExecutor { return items })
	reg.Register(app.typ, func() workflow.NodeExecutor { return app })
	reg.Register("test.rec", func() workflow.NodeExecutor { return after })
	return app, after, runWF(t, reg, wf, &recStore{}, "")
}

var threeNames = itemsNode{{"n": "a"}, {"n": "b"}, {"n": "c"}}

func TestPerItem_DefaultRunsOnceWithFirstItem(t *testing.T) {
	app, after, err := perItemRun(t, threeNames, newCallRec("service.x"),
		perItemWF(nil, map[string]interface{}{"q": "{{ $json.n }}"}, "service.x"))
	if err != nil {
		t.Fatal(err)
	}
	if !eqStrs(*app.calls, []string{"a"}) || !eqInts(*app.sizes, []int{3}) {
		t.Fatalf("calls %v sizes %v, want one call with 3 items", *app.calls, *app.sizes)
	}
	if len(*after.items) != 1 {
		t.Fatalf("downstream got %d items, want 1", len(*after.items))
	}
}

func TestPerItem_NodeFlagRunsEachItem(t *testing.T) {
	app, after, err := perItemRun(t, threeNames, newCallRec("test.app"),
		perItemWF(nil, map[string]interface{}{"q": "{{ $json.n }}", "execute_once_per_item": true}, "test.app"))
	if err != nil {
		t.Fatal(err)
	}
	if !eqStrs(*app.calls, []string{"a", "b", "c"}) || !eqInts(*app.sizes, []int{1, 1, 1}) {
		t.Fatalf("calls %v sizes %v", *app.calls, *app.sizes)
	}
	if len(*after.items) != 3 || (*after.items)[2].JSON["q"] != "c" {
		t.Fatalf("downstream items %v", *after.items)
	}
}

func TestPerItem_WorkflowSettingCoversAppNodesOnly(t *testing.T) {
	on := map[string]interface{}{workflow.PerItemExecutionSetting: true}
	cfg := map[string]interface{}{"q": "{{ $json.n }}"}
	app, _, err := perItemRun(t, threeNames, newCallRec("service.x"), perItemWF(on, cfg, "service.x"))
	if err != nil || len(*app.calls) != 3 {
		t.Fatalf("service node: err %v calls %v, want 3 calls", err, *app.calls)
	}
	core, _, err := perItemRun(t, threeNames, newCallRec("core.x"), perItemWF(on, cfg, "core.x"))
	if err != nil || len(*core.calls) != 1 {
		t.Fatalf("core node: err %v calls %v, want 1 call", err, *core.calls)
	}
	optOut := map[string]interface{}{"q": "x", "execute_once_per_item": false}
	app2, _, err := perItemRun(t, threeNames, newCallRec("service.x"), perItemWF(on, optOut, "service.x"))
	if err != nil || len(*app2.calls) != 1 {
		t.Fatalf("opted-out node: err %v calls %v, want 1 call", err, *app2.calls)
	}
}

func TestPerItem_OnErrorPerItem(t *testing.T) {
	items := itemsNode{{"n": "a"}, {"n": "b", "fail": true}, {"n": "c"}}
	for _, tc := range []struct {
		onError string
		want    int // items reaching the next node
	}{{"continue", 3}, {"skip", 2}} {
		app, after, err := perItemRun(t, items, newCallRec("test.app"), perItemWF(nil, map[string]interface{}{
			"q": "{{ $json.n }}", "execute_once_per_item": true, "on_error": tc.onError,
		}, "test.app"))
		var pf *workflow.PartialFailureError
		if !errors.As(err, &pf) {
			t.Fatalf("%s: err = %v, want a partial failure", tc.onError, err)
		}
		if len(*app.calls) != 3 || len(*after.items) != tc.want {
			t.Fatalf("%s: calls %v, downstream %d items, want 3 calls and %d", tc.onError, *app.calls, len(*after.items), tc.want)
		}
	}
	app, after, err := perItemRun(t, items, newCallRec("test.app"), perItemWF(nil, map[string]interface{}{
		"q": "{{ $json.n }}", "execute_once_per_item": true,
	}, "test.app"))
	if err == nil || len(*app.calls) != 2 || len(*after.items) != 0 {
		t.Fatalf("stop: err %v calls %v downstream %d, want failure after the 2nd call", err, *app.calls, len(*after.items))
	}
}

func TestExecuteOnce_FirstItemOnly(t *testing.T) {
	for _, key := range []string{"execute_once", "executeOnce"} {
		app, _, err := perItemRun(t, threeNames, newCallRec("test.app"),
			perItemWF(nil, map[string]interface{}{"q": "{{ $json.n }}", key: true}, "test.app"))
		if err != nil {
			t.Fatal(err)
		}
		if !eqStrs(*app.calls, []string{"a"}) || !eqInts(*app.sizes, []int{1}) {
			t.Fatalf("%s: calls %v sizes %v, want one call with 1 item", key, *app.calls, *app.sizes)
		}
	}
}
