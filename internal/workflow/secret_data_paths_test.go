package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/vault"
)

// emitNode hands on the items it holds, whatever came in.
type emitNode struct{ items []map[string]interface{} }

func (emitNode) Type() string { return "test.emit" }
func (n emitNode) Execute(context.Context, NodeInput, map[string]interface{}) ([]NodeOutput, error) {
	var out []Item
	for _, m := range n.items {
		out = append(out, NewItem(m))
	}
	return []NodeOutput{{Handle: "main", Items: out}}, nil
}

// subRoot hands its sub-nodes' configs on; with call, it calls its ai_tool
// sub-node once per item, with the item's msg as the tool's q.
type subRoot struct {
	call bool
	got  *[]map[string]interface{}
}

func (subRoot) Type() string { return "test.subroot" }
func (r subRoot) Execute(ctx context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	for _, s := range in.SubNodes {
		if !r.call {
			*r.got = append(*r.got, s.Config)
			continue
		}
		for _, it := range in.Items {
			items, err := s.Call(ctx, map[string]interface{}{"q": it.JSON["msg"]})
			if err != nil {
				return nil, err
			}
			for _, out := range items {
				*r.got = append(*r.got, out.JSON)
			}
		}
	}
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// pathsFixture is an approved workflow's run over a vault holding vs.
type pathsFixture struct {
	ctx   context.Context
	store *SQLiteWorkflowStore
}

func newPathsFixture(t *testing.T) pathsFixture {
	t.Helper()
	keyring.MockInit()
	db, store := approvalsTestDB(t)
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	if _, err := secrets.Add(ctx, db.DB, "default", "secret", "vs", map[string]string{"secret": "vsecret-777"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	return pathsFixture{ctx, store}
}

// run saves nodes behind a manual trigger (the first one connected from it,
// the rest by conns), approves the workflow and runs it with input.
func (f pathsFixture) run(t *testing.T, reg *NodeTypeRegistry, input map[string]interface{}, nodes []WorkflowNode, conns ...WorkflowConnection) error {
	t.Helper()
	id := "wf-" + uuid.NewString()[:8]
	// Node ids are unique across workflows: each is the workflow's id and its own.
	wf := &Workflow{ID: id, Name: id, ProfileID: "default", Nodes: []WorkflowNode{{ID: id + "-t", WorkflowID: id, Type: "trigger.manual", Name: "Start"}}}
	for _, n := range nodes {
		n.ID, n.WorkflowID = id+"-"+n.ID, id
		wf.Nodes = append(wf.Nodes, n)
	}
	wf.Connections = append([]WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: nodes[0].ID, TargetHandle: "main"}}, conns...)
	for i := range wf.Connections {
		c := &wf.Connections[i]
		c.ID, c.WorkflowID, c.SourceNodeID, c.TargetNodeID = fmt.Sprintf("%s-c%d", id, i), id, id+"-"+c.SourceNodeID, id+"-"+c.TargetNodeID
	}
	createSQLWorkflow(t, f.store, wf)
	if err := f.store.ApproveDefinition(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	exec := &WorkflowExecution{ID: uuid.NewString(), WorkflowID: id, ProfileID: "default", TriggerType: "trigger.manual", TriggerNodeID: id + "-t", TriggerData: input}
	return RunExecution(f.ctx, exec, wf, dag, reg, f.store, nil, NewExpressionEngine(), zerolog.Nop())
}

// Each path that resolves a step's config against data checks it against
// the config as the workflow holds it: run once per item (the second item
// carries the reference, so the batch's first-item pass doesn't see it), a
// sub-node's config, and an ai_tool sub-node's config when its root calls it
// (#845 code review, 3).
func TestEveryConfigPathRefusesAReferenceFromData(t *testing.T) {
	f := newPathsFixture(t)

	t.Run("once per item", func(t *testing.T) {
		var got []map[string]interface{}
		reg := NewNodeTypeRegistry()
		reg.Register("test.emit", func() NodeExecutor {
			return emitNode{[]map[string]interface{}{{"msg": "hello"}, {"msg": "@secret:vs"}}}
		})
		reg.Register("test.configprobe", func() NodeExecutor { return configProbe{&got} })
		err := f.run(t, reg, map[string]interface{}{}, []WorkflowNode{
			{ID: "e", Type: "test.emit", Name: "Emit"},
			{ID: "p", Type: "test.configprobe", Name: "Probe", Config: map[string]interface{}{"msg": "{{ $json.msg }}", "own": "@secret:vs", "execute_once_per_item": true}},
		}, WorkflowConnection{SourceNodeID: "e", SourceHandle: "main", TargetNodeID: "p", TargetHandle: "main"})
		if !errors.Is(err, secrets.ErrDataReference) {
			t.Errorf("run %v, want the second item refused", err)
		}
		for _, c := range got {
			if c["msg"] != "hello" || c["own"] != "vsecret-777" {
				t.Errorf("the step ran with %v, want only the first item, with its own reference resolved", c)
			}
		}
	})

	t.Run("a sub-node", func(t *testing.T) {
		var got []map[string]interface{}
		reg := NewNodeTypeRegistry()
		reg.Register("test.subroot", func() NodeExecutor { return subRoot{got: &got} })
		reg.Register("test.configprobe", func() NodeExecutor { return configProbe{&got} })
		err := f.run(t, reg, map[string]interface{}{"msg": "@secret:vs"}, []WorkflowNode{
			{ID: "r", Type: "test.subroot", Name: "Root"},
			{ID: "m", Type: "test.configprobe", Name: "Model", Config: map[string]interface{}{"model": "{{ $json.msg }}"}},
		}, WorkflowConnection{SourceNodeID: "m", SourceHandle: "ai_languageModel", TargetNodeID: "r", TargetHandle: "ai_languageModel"})
		if !errors.Is(err, secrets.ErrDataReference) || !strings.Contains(err.Error(), "-m (Model)") || len(got) != 0 {
			t.Errorf("run %v, the root got %v", err, got)
		}
	})

	t.Run("an ai_tool its root calls", func(t *testing.T) {
		var got []map[string]interface{}
		reg := NewNodeTypeRegistry()
		reg.Register("test.subroot", func() NodeExecutor { return subRoot{call: true, got: &got} })
		reg.Register("test.configprobe", func() NodeExecutor { return configProbe{&[]map[string]interface{}{}} })
		run := func(msg string) error {
			return f.run(t, reg, map[string]interface{}{"msg": msg}, []WorkflowNode{
				{ID: "r", Type: "test.subroot", Name: "Root"},
				{ID: "w", Type: "test.configprobe", Name: "Tool", Config: map[string]interface{}{"q": "{{ $json.q }}", "own": "@secret:vs"}},
			}, WorkflowConnection{SourceNodeID: "w", SourceHandle: "ai_tool", TargetNodeID: "r", TargetHandle: "ai_tool"})
		}
		if err := run("@secret:vs"); !errors.Is(err, secrets.ErrDataReference) || !strings.Contains(err.Error(), `tool "Tool"`) || len(got) != 0 {
			t.Errorf("run %v, the tool gave %v", err, got)
		}
		if err := run("hello"); err != nil || len(got) != 1 || got[0]["q"] != "hello" || got[0]["own"] != "vsecret-777" {
			t.Errorf("run %v, the tool gave %v, want its own reference resolved", err, got)
		}
	})
}
