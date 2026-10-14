package dbnodes

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Every mapping must point at a registered node type (or a trigger with a
// subscriber), and every mapped config key must be one the node reads.
func TestN8nMappingsTargetRealTypes(t *testing.T) {
	reg := workflow.NewNodeTypeRegistry()
	RegisterAll(reg)
	for n8nType, m := range N8nNodeMappings {
		if !strings.HasPrefix(n8nType, "n8n-nodes-base.") {
			t.Errorf("%s: not an n8n type name", n8nType)
		}
		if m.Trigger {
			if _, ok := TriggerSubscribers[m.MonoType]; !ok {
				t.Errorf("%s -> %s: no subscriber", n8nType, m.MonoType)
			}
			continue
		}
		if _, ok := reg.Get(m.MonoType); !ok {
			t.Errorf("%s -> %s: not registered", n8nType, m.MonoType)
		}
		if len(m.Params) == 0 {
			t.Errorf("%s: no params", n8nType)
		}
	}
}

func TestToKVMapShapes(t *testing.T) {
	m, err := toKVMap(map[string]interface{}{"header": []interface{}{map[string]interface{}{"key": "a", "value": "1"}}})
	if err != nil || m["a"] != "1" {
		t.Errorf("wrapped list = %v %v", m, err)
	}
	m, err = toKVMap([]interface{}{map[string]interface{}{"column": "c", "value": 2}})
	if err != nil || m["c"] != 2 {
		t.Errorf("column list = %v %v", m, err)
	}
	if _, err := toKVMap([]interface{}{map[string]interface{}{"value": 2}}); err == nil {
		t.Error("missing key: want error")
	}
}
