package n8nimport_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/nodes/control"
	"github.com/monoes/mono-agent/internal/workflow"
)

// n8n's Python Code node (Pyodide "python" and task-runner "pythonNative")
// imports as core.code with language python and its code unchanged, and
// the imported node runs n8n's default code for each mode.
func TestCodePythonImports(t *testing.T) {
	cases := []struct {
		lang, mode, code string
		version          int
	}{
		{"python", "runOnceForAllItems", "for item in _input.all():\n  item.json.myNewField = 1\nreturn _input.all()", 1},
		{"pythonNative", "runOnceForAllItems", "for item in _items:\n  item[\"json\"][\"my_new_field\"] = 1\nreturn _items", 2},
		{"pythonNative", "runOnceForEachItem", "_item[\"json\"][\"my_new_field\"] = 1\nreturn _item", 2},
	}
	_, pyErr := exec.LookPath("python3")
	for _, tc := range cases {
		p, _ := json.Marshal(map[string]interface{}{"language": tc.lang, "mode": tc.mode, "pythonCode": tc.code})
		src := fmt.Sprintf(`{"name":"p","nodes":[{"name":"Py","type":"n8n-nodes-base.code","typeVersion":%d,"position":[0,0],"parameters":%s}],"connections":{}}`, tc.version, p)
		wf, rep, err := n8nimport.Convert([]byte(src))
		if err != nil {
			t.Fatalf("convert: %v", err)
		}
		n, nr := node(t, wf, "Py"), nodeReport(t, rep, "Py")
		if n.Type != "core.code" || nr.Status != n8nimport.StatusFull {
			t.Fatalf("%s/%s: type %s, status %s:\n%s", tc.lang, tc.mode, n.Type, nr.Status, issueText(rep, "Py"))
		}
		if n.Config["language"] != "python" || n.Config["code"] != tc.code {
			t.Errorf("%s: config %v", tc.lang, n.Config)
		}
		if (tc.mode == "runOnceForEachItem") != (n.Config["mode"] == "runOnceForEachItem") {
			t.Errorf("%s: mode %v", tc.mode, n.Config["mode"])
		}
		if pyErr != nil {
			continue
		}
		in := workflow.NodeInput{Items: []workflow.Item{{JSON: map[string]interface{}{"a": 1.0}}, {JSON: map[string]interface{}{"a": 2.0}}}}
		out, err := (&control.CodeNode{}).Execute(context.Background(), in, n.Config)
		if err != nil {
			t.Fatalf("%s/%s: run: %v", tc.lang, tc.mode, err)
		}
		if items := out[0].Items; len(items) != 2 || items[1].JSON["a"] != 2.0 || len(items[1].JSON) != 2 {
			t.Errorf("%s/%s: items %v", tc.lang, tc.mode, items)
		}
	}
}
