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
