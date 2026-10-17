package commsync

import "encoding/json"

// OutboundKey records, in a workflow Human-in-Loop item's node_config,
// whether its approval lets a message to a person go out: true when the nodes
// after the Human-in-Loop node may send (an email, a DM, a comment, a post, a
// connect or a publish, classified the way an agent's sends are), false when
// they were checked and don't. The node writes it, never the workflow's
// author; only the operator decides an item whose value is true
// (operator.ItemRefusal, `hil approve|reject`).
const OutboundKey = "outbound"

// OutboundHIL reports whether a workflow Human-in-Loop item, with
// node_config nc, lets a message to a person go out: its recorded decision,
// or, when it has none (an item queued before the decision was recorded, or a
// node_config that can't be read), true: what was never checked is the
// operator's to decide.
func OutboundHIL(nc string) bool {
	var cfg map[string]any
	if json.Unmarshal([]byte(nc), &cfg) != nil {
		return true
	}
	decided, ok := cfg[OutboundKey].(bool)
	return !ok || decided
}
