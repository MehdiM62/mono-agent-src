package n8nimport_test

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/monoes/mono-agent/internal/nodes/control"
	"github.com/monoes/mono-agent/internal/workflow"
)

// QA-A U8: an imported Code node read $runIndex as 0 on every loop pass;
// the prelude now reads the run index core.code is given.
func TestImportedCodeSeesRunIndex(t *testing.T) {
	const src = `{"name":"t","nodes":[
{"id":"1","name":"Start","type":"n8n-nodes-base.manualTrigger","typeVersion":1,"position":[0,0],"parameters":{}},
{"id":"2","name":"Pass","type":"n8n-nodes-base.code","typeVersion":2,"position":[200,0],"parameters":{"jsCode":"return [{json:{run: $runIndex}}];"}}
],"connections":{"Start":{"main":[[{"node":"Pass","type":"main","index":0}]]}}}`
	wf, _, err := n8nimport.Convert([]byte(src))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	var code string
	for _, n := range wf.Nodes {
		if n.Name == "Pass" {
			code, err = workflow.NewExpressionEngine().EvaluateString(n.Config["code"].(string), workflow.ExpressionContext{})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
		}
	}
	for _, runIndex := range []int{0, 2} {
		out, err := (&control.CodeNode{}).Execute(context.Background(), workflow.NodeInput{RunIndex: runIndex}, map[string]interface{}{"code": code})
		if err != nil {
			t.Fatalf("run %d: %v", runIndex, err)
		}
		if got := toF(out[0].Items[0].JSON["run"]); got != float64(runIndex) {
			t.Errorf("$runIndex on run %d = %v", runIndex, out[0].Items[0].JSON["run"])
		}
	}
}
