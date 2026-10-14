package workflow

import (
	"errors"
	"testing"
)

func subWF() ([]WorkflowNode, []WorkflowConnection) {
	nodes := []WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
		{ID: "a", Type: "agent.ask", Name: "Ask"},
		{ID: "m", Type: "ai.model", Name: "Model"},
		{ID: "e", Type: "ai.embed_cfg", Name: "Embed"},
	}
	conns := []WorkflowConnection{
		{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "a", TargetHandle: "main"},
		{SourceNodeID: "m", SourceHandle: "ai_languageModel", TargetNodeID: "a", TargetHandle: "ai_languageModel"},
		{SourceNodeID: "e", SourceHandle: "ai_embedding", TargetNodeID: "m", TargetHandle: "ai_embedding"},
	}
	return nodes, conns
}

func TestDAGKeepsSubNodesOutOfTheFlow(t *testing.T) {
	d, err := BuildDAG(subWF())
	if err != nil {
		t.Fatal(err)
	}
	order, err := d.TopologicalSort()
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0].ID != "t" || order[1].ID != "a" {
		t.Fatalf("order = %v, want the trigger and the root only", order)
	}
	if !d.IsSubNode("m") || !d.IsSubNode("e") || d.IsSubNode("a") {
		t.Fatal("sub-node set wrong")
	}
	subs := d.SubNodes("a")
	if len(subs) != 1 || subs[0].Kind != "ai_languageModel" || subs[0].Node.ID != "m" {
		t.Fatalf("SubNodes(a) = %+v", subs)
	}
	if d.InDegree("a") != 1 || len(d.Successors("m")) != 0 {
		t.Fatal("sub-node edges leaked into the main flow")
	}
}

func TestDAGRejectsSubNodeInTheMainFlowAndSubCycles(t *testing.T) {
	nodes, conns := subWF()
	conns = append(conns, WorkflowConnection{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "m", TargetHandle: "main"})
	if _, err := BuildDAG(nodes, conns); !errors.Is(err, ErrSubNodeWiring) {
		t.Fatalf("mixed wiring: err = %v", err)
	}
	nodes, conns = subWF()
	conns = append(conns, WorkflowConnection{SourceNodeID: "m", SourceHandle: "ai_tool", TargetNodeID: "e", TargetHandle: "ai_tool"})
	if _, err := BuildDAG(nodes, conns); !errors.Is(err, ErrCycleDetected) {
		t.Fatalf("sub-node cycle: err = %v", err)
	}
}

// validate accepts ai_… connections outside the main ports.
func TestValidateAcceptsSubNodeConnections(t *testing.T) {
	w := &Workflow{Name: "sub", Nodes: []WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
		{ID: "a", Type: "agent.ask", Name: "Ask", Config: map[string]interface{}{"runtime": "claude", "prompt": "hi"}},
		{ID: "m", Type: "agent.ask", Name: "Model", Config: map[string]interface{}{"runtime": "claude", "prompt": "x"}},
	}, Connections: []WorkflowConnection{
		{ID: "c1", SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "a", TargetHandle: "main"},
		{ID: "c2", SourceNodeID: "m", SourceHandle: "ai_languageModel", TargetNodeID: "a", TargetHandle: "ai_languageModel"},
	}}
	if err := ValidateForActivation(w); err != nil {
		t.Fatalf("activation: %v", err)
	}
	if rep := ValidateWorkflow(w, ValidateOptions{}); !rep.Valid {
		t.Fatalf("validate: %v", rep.Errors)
	}
}
