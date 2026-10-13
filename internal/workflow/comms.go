package workflow

import (
	"strings"

	"github.com/monoes/mono-agent/internal/commsync"
)

const humanInLoopType = "core.human_in_loop"

// dagHasHIL reports whether the workflow has a Human-in-Loop node, i.e.
// whether any node's run can matter to a queued Communications message.
func dagHasHIL(dag *DAG) bool {
	for _, n := range dag.AllNodes() {
		if n.Type == humanInLoopType {
			return true
		}
	}
	return false
}

// downstreamOf returns the nodes a Human-in-Loop node's approval is for: the
// first node of each branch that is not a control node (core.*) and not
// disabled. Their configs are unresolved (templates intact).
func downstreamOf(dag *DAG, nodeID string) []commsync.Node {
	var out []commsync.Node
	seen := map[string]bool{nodeID: true}
	queue := []string{nodeID}
	for len(queue) > 0 && len(seen) < 200 {
		cur := queue[0]
		queue = queue[1:]
		for _, s := range dag.Successors(cur) {
			if seen[s.ID] || s.Disabled {
				continue
			}
			seen[s.ID] = true
			if strings.HasPrefix(s.Type, "core.") {
				queue = append(queue, s.ID)
				continue
			}
			n := s
			_ = n.ParseConfig()
			out = append(out, commsync.Node{ID: n.ID, Type: n.Type, Name: n.Name, Config: n.Config})
		}
	}
	return out
}
