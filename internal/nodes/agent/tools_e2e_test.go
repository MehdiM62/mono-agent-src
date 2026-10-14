package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/noderegistry"
	"github.com/monoes/mono-agent/internal/nodes/agent"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A node attached to agent.ask through ai_tool is a caller tool: the turn is
// offered it (named after the node, its arguments read from the {{ $json.x }}
// its config uses), a call runs the node once with the arguments as its item,
// and the output item lists the calls. The tool never runs as a step.
func TestAgentAskToolsEndToEnd(t *testing.T) {
	db := testdb.Open(t)
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "p1")
	var offered []monomind.ToolSpec
	var toolResult string
	agent.FakeTurnsForTest(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		offered = o.Tools
		if o.OnToolCall == nil {
			t.Fatal("no tool handler")
		}
		out, err := o.OnToolCall(context.Background(), "Weather", json.RawMessage(`{"city":"Paris"}`))
		if err != nil {
			t.Fatalf("tool call: %v", err)
		}
		toolResult = out
		return &monomind.TurnResult{ResultText: "It is sunny in Paris."}, nil
	})
	store := workflow.NewSQLiteWorkflowStore(db.DB)
	reg := noderegistry.Build(db.DB)
	wf := &workflow.Workflow{Name: "tools", ProfileID: "p1", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
		{ID: "a", Type: "agent.ask", Name: "Ask", Config: map[string]interface{}{"runtime": "claude", "prompt": "Weather in Paris?"}},
		{ID: "w", Type: "core.set", Name: "Weather", Config: map[string]interface{}{
			"tool_description": "Look up the weather of a city.",
			"assignments":      []interface{}{map[string]interface{}{"field": "forecast", "value": "{{ $json.city }}: sunny"}}}},
	}, Connections: []workflow.WorkflowConnection{
		{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "a", TargetHandle: "main"},
		{SourceNodeID: "w", SourceHandle: "ai_tool", TargetNodeID: "a", TargetHandle: "ai_tool"},
	}}
	if rep := workflow.ValidateWorkflow(wf, workflow.ValidateOptions{}); !rep.Valid {
		t.Fatalf("validate: %+v", rep.Issues)
	}
	if err := store.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	dag, err := workflow.BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	e := &workflow.WorkflowExecution{WorkflowID: wf.ID, ProfileID: "p1", TriggerType: "manual", TriggerData: map[string]interface{}{}}
	if err := store.CreateExecution(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := workflow.RunExecution(ctx, e, wf, dag, reg, store, nil, workflow.NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(offered) != 1 || offered[0].Name != "Weather" || offered[0].Description != "Look up the weather of a city." {
		t.Fatalf("offered tools %+v", offered)
	}
	props, _ := offered[0].Schema["properties"].(map[string]interface{})
	if _, ok := props["city"]; !ok {
		t.Errorf("schema has no city argument: %v", offered[0].Schema)
	}
	if !strings.Contains(toolResult, "Paris: sunny") {
		t.Errorf("tool result %q", toolResult)
	}
	nodes, err := store.GetExecutionNodeOutputs(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	var calls interface{}
	for _, n := range nodes {
		if n.NodeID == "w" {
			t.Fatal("the tool ran as a step")
		}
		if n.NodeID == "a" && len(n.OutputItems) == 1 {
			calls = n.OutputItems[0].JSON["_agent_tool_calls"]
		}
	}
	b, _ := json.Marshal(calls)
	if !strings.Contains(string(b), `"tool":"Weather"`) || !strings.Contains(string(b), `"ok":true`) {
		t.Errorf("_agent_tool_calls = %s", b)
	}
}
