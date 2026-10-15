package workflowrun

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func TestTruncateOutputItems(t *testing.T) {
	t.Parallel()
	small := workflow.Item{JSON: map[string]interface{}{"k": "v"}}
	big := workflow.Item{JSON: map[string]interface{}{"k": strings.Repeat("x", maxOutputItemBytes)}}

	got := truncateOutputItems([]workflow.Item{small, big})
	if len(got) != 2 {
		t.Fatalf("expected item count preserved (2), got %d", len(got))
	}
	if got[0].JSON["k"] != "v" {
		t.Fatalf("small item should pass through unchanged, got %+v", got[0])
	}
	truncated, _ := got[1].JSON["truncated"].(bool)
	if !truncated {
		t.Fatalf("big item should be truncated, got %+v", got[1])
	}
	if note, _ := got[1].JSON["note"].(string); note == "" {
		t.Fatal("truncated item should carry a note")
	}
}
