package workflow

import (
	"errors"
	"testing"
)

func handleConn(id, src, handle, dst string) WorkflowConnection {
	return WorkflowConnection{ID: id, SourceNodeID: src, SourceHandle: handle, TargetNodeID: dst, TargetHandle: "main"}
}

// A cycle closed from the loop body back into split_in_batches is the n8n
// "Loop Over Items" pattern and must build; the back-edge is excluded from
// the topological order.
func TestDAGLoopBackEdgeAllowed(t *testing.T) {
	nodes := []WorkflowNode{
		makeNode("T", "trigger.manual"),
		makeNode("S", LoopNodeType),
		makeNode("B", "action.b"),
		makeNode("D", "action.d"),
	}
	conns := []WorkflowConnection{
		handleConn("1", "T", "main", "S"),
		handleConn("2", "S", "loop", "B"),
		handleConn("3", "B", "main", "S"),
		handleConn("4", "S", "done", "D"),
	}
	dag, err := BuildDAG(nodes, conns)
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
	order, err := dag.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort: %v", err)
	}
	if got := nodeIDs(order); got[0] != "T" || got[1] != "S" {
		t.Errorf("order = %v, want T then S first", got)
	}
	if !dag.isBackEdge(conns[2]) {
		t.Errorf("B→S should be a back-edge")
	}
	if dag.isBackEdge(conns[0]) {
		t.Errorf("T→S should not be a back-edge")
	}
	if got := dag.forwardInDegree("S"); got != 1 {
		t.Errorf("forwardInDegree(S) = %d, want 1", got)
	}
	if !dag.loopBody("S")["B"] || dag.loopBody("S")["D"] {
		t.Errorf("loop body = %v, want {B}", dag.loopBody("S"))
	}
	if !dag.loopDeferred("S")["D"] {
		t.Errorf("deferred = %v, want D", dag.loopDeferred("S"))
	}
}

// The legacy "batch" handle is an alias of "loop".
func TestDAGLoopBatchHandleAlias(t *testing.T) {
	nodes := []WorkflowNode{makeNode("S", LoopNodeType), makeNode("B", "action.b")}
	conns := []WorkflowConnection{handleConn("1", "S", "batch", "B"), handleConn("2", "B", "main", "S")}
	if _, err := BuildDAG(nodes, conns); err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
}

// Nested loops: the outer loop's forward edge into the inner loop node is
// not a back-edge even though the inner loop's done branch leads back out.
func TestDAGNestedLoops(t *testing.T) {
	nodes := []WorkflowNode{
		makeNode("T", "trigger.manual"),
		makeNode("S1", LoopNodeType),
		makeNode("S2", LoopNodeType),
		makeNode("X", "action.x"),
		makeNode("Y", "action.y"),
		makeNode("D", "action.d"),
	}
	conns := []WorkflowConnection{
		handleConn("1", "T", "main", "S1"),
		handleConn("2", "S1", "loop", "S2"),
		handleConn("3", "S2", "loop", "X"),
		handleConn("4", "X", "main", "S2"),
		handleConn("5", "S2", "done", "Y"),
		handleConn("6", "Y", "main", "S1"),
		handleConn("7", "S1", "done", "D"),
	}
	dag, err := BuildDAG(nodes, conns)
	if err != nil {
		t.Fatalf("BuildDAG: %v", err)
	}
	if dag.isBackEdge(conns[1]) {
		t.Errorf("S1→S2 must be a forward edge")
	}
	if !dag.isBackEdge(conns[3]) || !dag.isBackEdge(conns[5]) {
		t.Errorf("X→S2 and Y→S1 must be back-edges")
	}
	body := dag.loopBody("S1")
	for _, id := range []string{"S2", "X", "Y"} {
		if !body[id] {
			t.Errorf("S1 body missing %s: %v", id, body)
		}
	}
}

// Cycles that do not pass through a loop node's loop output stay errors.
func TestDAGCycleRejectedOutsideLoop(t *testing.T) {
	cases := map[string]struct {
		nodes []WorkflowNode
		conns []WorkflowConnection
	}{
		"plain cycle": {
			nodes: []WorkflowNode{makeNode("A", "action.a"), makeNode("B", "action.b")},
			conns: []WorkflowConnection{handleConn("1", "A", "main", "B"), handleConn("2", "B", "main", "A")},
		},
		"cycle through done": {
			nodes: []WorkflowNode{makeNode("S", LoopNodeType), makeNode("D", "action.d")},
			conns: []WorkflowConnection{handleConn("1", "S", "done", "D"), handleConn("2", "D", "main", "S")},
		},
		"cycle inside loop body": {
			nodes: []WorkflowNode{makeNode("S", LoopNodeType), makeNode("A", "action.a"), makeNode("B", "action.b")},
			conns: []WorkflowConnection{
				handleConn("1", "S", "loop", "A"),
				handleConn("2", "A", "main", "B"),
				handleConn("3", "B", "main", "A"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildDAG(tc.nodes, tc.conns); !errors.Is(err, ErrCycleDetected) {
				t.Errorf("err = %v, want ErrCycleDetected", err)
			}
		})
	}
}
