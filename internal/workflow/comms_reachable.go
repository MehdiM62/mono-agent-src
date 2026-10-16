package workflow

import (
	"strings"

	"github.com/monoes/mono-agent/internal/commsync"
)

const executeWorkflowType = "core.execute_workflow"

// Bounds of reachableFrom: sub-workflows nested deeper, or more nodes than
// this, leave the rest unresolved (and so count as sending).
const (
	reachableMaxDepth = 8
	reachableMaxNodes = 5000
)

// reachableFrom is everything that can run once Human-in-Loop node nodeID's
// items are approved (commsync.Reachable): every node reachable from it,
// control and disabled nodes included, and every node of a workflow one of
// those calls (core.execute_workflow), resolved the way the call resolves
// it. A call whose workflow can't be resolved here makes the set Unresolved:
// the HIL node then fails closed. Unlike downstreamOf (what Communications
// queues), it doesn't stop at the first node of a branch.
func (r *runner) reachableFrom(nodeID string) commsync.Reachable {
	var out commsync.Reachable
	seen := map[string]bool{nodeID: true}
	var nodes []WorkflowNode
	queue := []string{nodeID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, s := range r.dag.Successors(cur) {
			if !seen[s.ID] {
				seen[s.ID] = true
				nodes = append(nodes, s)
				queue = append(queue, s.ID)
			}
		}
	}
	r.addReachable(&out, nodes, map[string]bool{r.wf.ID: true}, 0)
	return out
}

// addReachable adds nodes to out, and the nodes of every workflow they call.
// called holds the workflows on the path to here, so a workflow calling
// itself ends.
func (r *runner) addReachable(out *commsync.Reachable, nodes []WorkflowNode, called map[string]bool, depth int) {
	for _, n := range nodes {
		if len(out.Nodes) >= reachableMaxNodes {
			out.Unresolved = true
			return
		}
		_ = n.ParseConfig()
		out.Nodes = append(out.Nodes, commsync.Node{ID: n.ID, Type: n.Type, Name: n.Name, Config: n.Config})
		if n.Type != executeWorkflowType {
			continue
		}
		sub := r.calledWorkflow(n.Config)
		switch {
		case sub == nil || depth+1 > reachableMaxDepth:
			out.Unresolved = true
		case called[sub.ID]:
			// Already on this path: its nodes are in out.
		default:
			called[sub.ID] = true
			r.addReachable(out, sub.Nodes, called, depth+1)
			delete(called, sub.ID)
		}
	}
}

// calledWorkflow is the workflow a core.execute_workflow node's config names,
// resolved as StartSubWorkflow does (an ID, then an exact name within the
// run's profile), or nil when it can't be told here: an expression, none, an
// ambiguous name, another profile's, or a store error.
func (r *runner) calledWorkflow(config map[string]interface{}) *Workflow {
	ref, _ := config["workflowId"].(string)
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.Contains(ref, "{{") || r.store == nil {
		return nil
	}
	ctx, cancel := dbCtx()
	defer cancel()
	profile := r.exec.ProfileID
	if wf, err := r.store.GetWorkflow(ctx, ref); err != nil {
		return nil
	} else if wf != nil {
		if ok, err := OwnedBy(wf, profile); err != nil || !ok {
			return nil
		}
		return wf
	}
	all, err := r.store.ListWorkflows(ctx, profile)
	if err != nil {
		return nil
	}
	var match *Workflow
	for i := range all {
		if all[i].Name == ref {
			if match != nil {
				return nil
			}
			match = &all[i]
		}
	}
	if match == nil {
		return nil
	}
	wf, err := r.store.GetWorkflow(ctx, match.ID)
	if err != nil || wf == nil {
		return nil
	}
	return wf
}
