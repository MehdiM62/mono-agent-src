package hilsend

import (
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A node is classified under the type that runs it, whatever name it is
// asked about by: the engine asks with its executor's type, `node run` and
// MCP node_run with the name as given (an unprefixed "telegram", a legacy
// "comm.discord"), and MaySend's callers with the names a workflow saved.
// The send table is keyed by the canonical types.

// blankOps is the operation a canonical type runs for a config that names
// none, a legacy one: comm.discord's default send, and comm.outlook_send's
// configs, which carry a message's keys where comm.outlook_read's carry
// none.
var blankOps = map[string]func(config map[string]interface{}) string{
	"service.discord": func(map[string]interface{}) string { return "send_message" },
	"service.outlook_mail": func(config map[string]interface{}) string {
		for _, k := range []string{"to", "subject", "body"} {
			if _, ok := config[k]; ok {
				return "send_message"
			}
		}
		return "list_messages"
	},
}

// canonical is nodeType and config as a send is classified: the type that
// runs nodeType (workflow.CanonicalNodeType), and config with the operation
// that type runs when config names none.
func canonical(nodeType string, config map[string]interface{}) (string, map[string]interface{}) {
	nodeType = workflow.CanonicalNodeType(nodeType)
	if blank, ok := blankOps[nodeType]; ok && opOf(config) == "" {
		c := cloneMap(config)
		c["operation"] = blank(config)
		config = c
	}
	return nodeType, config
}

// opOf is the operation config names as the node reads it
// (workflow.ConfigOperation): a value that is no string names none, so on a
// node whose blank operation sends it is that send. It is trimmed, which
// only ever judges more as a send.
func opOf(config map[string]interface{}) string {
	return strings.TrimSpace(workflow.ConfigOperation(config))
}
