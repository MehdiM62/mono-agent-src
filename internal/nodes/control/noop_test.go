package control

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func TestNoOpPassesItemsThrough(t *testing.T) {
	in := []workflow.Item{{JSON: map[string]interface{}{"a": 1}}, {JSON: map[string]interface{}{"a": 2}}}
	out, err := (&NoOpNode{}).Execute(context.Background(), workflow.NodeInput{Items: in}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Handle != "main" || len(out[0].Items) != 2 || out[0].Items[1].JSON["a"] != 2 {
		t.Fatalf("outputs %+v", out)
	}
}
