package commsync

import "context"

// Node is a workflow node that runs after a Human-in-Loop node: what the
// engine hands the HIL node so it can tell what the approval is for.
type Node struct {
	ID     string                 `json:"id"`
	Type   string                 `json:"type"`
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config"`
}

type downstreamKey struct{}

type downstream struct {
	workflow string
	nodes    []Node
}

// WithDownstream attaches the name of the workflow and the nodes that run
// after a HIL node (the first non-control node of each branch) to ctx.
func WithDownstream(ctx context.Context, workflowName string, nodes []Node) context.Context {
	return context.WithValue(ctx, downstreamKey{}, downstream{workflow: workflowName, nodes: nodes})
}

// DownstreamFrom returns what WithDownstream attached. Callers must not
// modify the nodes.
func DownstreamFrom(ctx context.Context) (workflowName string, nodes []Node) {
	d, _ := ctx.Value(downstreamKey{}).(downstream)
	return d.workflow, d.nodes
}

type reachableKey struct{}

// Reachable is everything that can run once a Human-in-Loop node's items are
// approved: every node reachable from it in its workflow, control nodes and
// disabled ones included, and the nodes of the workflows those call
// (core.execute_workflow), as far as the engine could resolve them. Unresolved
// says something reachable could not be resolved (a sub-workflow named by an
// expression, missing, ambiguous or nested too deep), so the node counts its
// items as outbound.
type Reachable struct {
	Nodes      []Node
	Unresolved bool
}

// WithReachable attaches what can run after a HIL node to ctx.
func WithReachable(ctx context.Context, r Reachable) context.Context {
	return context.WithValue(ctx, reachableKey{}, r)
}

// ReachableFrom returns what WithReachable attached; ok is false outside an
// engine run. Callers must not modify the nodes.
func ReachableFrom(ctx context.Context) (r Reachable, ok bool) {
	r, ok = ctx.Value(reachableKey{}).(Reachable)
	return r, ok
}
