package control

import (
	"context"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Conditions a control node evaluates itself see $now in the workflow's
// timezone (NodeInput.Location).
func TestFilterNowUsesInputLocation(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	out, err := (&FilterNode{}).Execute(context.Background(), workflow.NodeInput{
		Items:    []workflow.Item{{JSON: map[string]interface{}{}}},
		Location: tokyo,
	}, map[string]interface{}{"condition": `{{ eq $now.Location.String "Asia/Tokyo" }}`})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || len(out[0].Items) != 1 {
		t.Fatalf("outputs %+v, want the item kept", out)
	}
}
