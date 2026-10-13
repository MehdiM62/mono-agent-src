package workflow

// LoopNodeType is the node type that may close a cycle: n8n's "Loop Over
// Items" (core.split_in_batches). Its "loop" output (legacy name "batch")
// feeds the loop body; an edge from the body back into the node starts the
// next iteration; its "done" output fires once after the last batch.
const LoopNodeType = "core.split_in_batches"

// isLoopHandle reports whether a loop node's output handle feeds its body.
func isLoopHandle(handle string) bool { return handle == "loop" || handle == "batch" }

type edgeKey struct{ src, srcHandle, dst, dstHandle string }

func keyOf(c WorkflowConnection) edgeKey {
	return edgeKey{c.SourceNodeID, c.SourceHandle, c.TargetNodeID, c.TargetHandle}
}

// loopIndex is the loop structure of a DAG, computed once by BuildDAG.
type loopIndex struct {
	back   map[edgeKey]bool
	fwdIn  map[string]int             // nodeID → in-degree without back-edges
	body   map[string]map[string]bool // loop node → nodes re-run each iteration
	after  map[string]map[string]bool // loop node → nodes held until it is done
	hasBak map[string]bool            // loop node → has at least one back-edge
}

// buildLoopIndex finds the back-edges and, per loop node, its body and the
// nodes that wait for its "done" output.
//
// An edge u→L into a loop node L is a back-edge when u is reachable from
// L's loop output without passing through L itself. Any other cycle is left
// in place for TopologicalSort to reject.
func buildLoopIndex(d *DAG) loopIndex {
	li := loopIndex{
		back:   make(map[edgeKey]bool),
		fwdIn:  make(map[string]int, len(d.nodes)),
		body:   make(map[string]map[string]bool),
		after:  make(map[string]map[string]bool),
		hasBak: make(map[string]bool),
	}
	for id, n := range d.nodes {
		if n.Type != LoopNodeType {
			continue
		}
		reach := d.reachFrom(id, isLoopHandle, nil)
		for _, c := range d.inEdges[id] {
			if c.SourceNodeID == id || reach[c.SourceNodeID] {
				li.back[keyOf(c)] = true
				li.hasBak[id] = true
			}
		}
	}
	for id := range d.nodes {
		for _, c := range d.inEdges[id] {
			if !li.back[keyOf(c)] {
				li.fwdIn[id]++
			}
		}
	}
	for id, n := range d.nodes {
		if n.Type != LoopNodeType {
			continue
		}
		loopReach := d.reachFrom(id, isLoopHandle, li.back)
		doneReach := d.reachFrom(id, func(h string) bool { return !isLoopHandle(h) }, li.back)
		body := make(map[string]bool)
		for n := range loopReach {
			if !doneReach[n] && n != id {
				body[n] = true
			}
		}
		delete(doneReach, id)
		li.body[id] = body
		li.after[id] = doneReach
	}
	return li
}

// reachFrom returns the nodes reachable from start through start's output
// edges whose handle satisfies first, then through any edge not in skip.
// start itself is never expanded again.
func (d *DAG) reachFrom(start string, first func(string) bool, skip map[edgeKey]bool) map[string]bool {
	seen := make(map[string]bool)
	var queue []string
	for _, c := range d.outEdges[start] {
		if first(c.SourceHandle) && !skip[keyOf(c)] && !seen[c.TargetNodeID] {
			seen[c.TargetNodeID] = true
			queue = append(queue, c.TargetNodeID)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == start {
			continue
		}
		for _, c := range d.outEdges[cur] {
			if skip[keyOf(c)] || seen[c.TargetNodeID] {
				continue
			}
			seen[c.TargetNodeID] = true
			queue = append(queue, c.TargetNodeID)
		}
	}
	return seen
}

// isBackEdge reports whether c closes a loop into a loop node.
func (d *DAG) isBackEdge(c WorkflowConnection) bool { return d.loops.back[keyOf(c)] }

// forwardInDegree is InDegree without loop back-edges.
func (d *DAG) forwardInDegree(nodeID string) int { return d.loops.fwdIn[nodeID] }

// isLoopNode reports whether nodeID is a loop node.
func (d *DAG) isLoopNode(nodeID string) bool {
	n, ok := d.nodes[nodeID]
	return ok && n.Type == LoopNodeType
}

// loopBody returns the nodes a loop node re-runs on every iteration: those
// reachable from its loop output and not from its other outputs.
func (d *DAG) loopBody(loopID string) map[string]bool { return d.loops.body[loopID] }

// loopDeferred returns the nodes reachable from a loop node's non-loop
// outputs ("done"); they run only once the loop has finished.
func (d *DAG) loopDeferred(loopID string) map[string]bool { return d.loops.after[loopID] }

// loopHasBackEdge reports whether any edge closes the loop into loopID.
func (d *DAG) loopHasBackEdge(loopID string) bool { return d.loops.hasBak[loopID] }

// forwardSuccessors returns the targets of nodeID's edges that are not
// loop back-edges.
func (d *DAG) forwardSuccessors(nodeID string) []WorkflowNode {
	var out []WorkflowNode
	for _, c := range d.outEdges[nodeID] {
		if d.isBackEdge(c) {
			continue
		}
		if n, ok := d.nodes[c.TargetNodeID]; ok {
			out = append(out, *n)
		}
	}
	return out
}
