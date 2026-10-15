package workflow

import (
	"encoding/json"
	"strings"
	"sync"
)

// declaresSubOutput reports whether n's schema (embedded in the node, or its
// type's schema) declares a sub_output: such a node is a sub-node even with
// nothing attached, so it never runs as a step. Trigger nodes never are.
func declaresSubOutput(n WorkflowNode) bool {
	if strings.HasPrefix(n.Type, "trigger.") {
		return false
	}
	if n.Schema != nil && n.Schema.SubOutput != "" {
		return true
	}
	return typeSubOutput(n.Type) != ""
}

var subOutputCache sync.Map // node type → sub_output ("" when none)

// typeSubOutput is the sub_output a node type's schema file (or schema
// provider) declares, read once per type.
func typeSubOutput(nodeType string) string {
	if v, ok := subOutputCache.Load(nodeType); ok {
		return v.(string)
	}
	data, ok := schemaFile(nodeType)
	if pd, pok := providedSchema(nodeType, data); pok {
		data, ok = pd, true
	}
	var s struct {
		SubOutput string `json:"sub_output"`
	}
	if ok {
		_ = json.Unmarshal(data, &s)
	}
	subOutputCache.Store(nodeType, s.SubOutput)
	return s.SubOutput
}
