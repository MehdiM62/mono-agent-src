package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

func runOutputWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		Nodes: []workflow.WorkflowNode{
			{ID: "t", Name: "Trigger", Type: "trigger.manual"},
			{ID: "f", Name: "Fetch", Type: "http.request"},
			{ID: "k", Name: "Keep fields", Type: "core.set"},
		},
		Connections: []workflow.WorkflowConnection{
			{SourceNodeID: "t", TargetNodeID: "f"}, {SourceNodeID: "f", TargetNodeID: "k"},
		},
	}
}

func items(n int) []workflow.Item {
	out := make([]workflow.Item, n)
	for i := range out {
		out[i] = workflow.Item{JSON: map[string]interface{}{"i": i}}
	}
	return out
}

// Only the leaf's output is shown, not the trigger's or an inner node's.
func TestPrintRunOutputShowsTheLeafNode(t *testing.T) {
	s := executionSummaryJSON{Status: "SUCCESS", Nodes: []executionNodeJSON{
		{NodeID: "t", Type: "trigger.manual", Status: "SUCCESS", OutputItems: items(1)},
		{NodeID: "f", Type: "http.request", Status: "SUCCESS", OutputItems: []workflow.Item{{JSON: map[string]interface{}{"raw": "body"}}}},
		{NodeID: "k", Type: "core.set", Status: "SUCCESS", OutputItems: []workflow.Item{{JSON: map[string]interface{}{"name": "Leanne Graham"}}}},
	}}
	var out bytes.Buffer
	printRunOutput(&out, s, runOutputWorkflow())
	want := "\nOutput (Keep fields):\n{\n  \"name\": \"Leanne Graham\"\n}\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestPrintRunOutputCapsItemsAndLines(t *testing.T) {
	s := executionSummaryJSON{Status: "SUCCESS_WITH_ERRORS", Nodes: []executionNodeJSON{
		{NodeID: "k", Type: "core.set", Status: "SUCCESS", OutputItems: items(8)},
	}}
	var out bytes.Buffer
	printRunOutput(&out, s, runOutputWorkflow())
	if got := strings.Count(out.String(), `"i":`); got != runOutputMaxItems {
		t.Errorf("shown %d items, want %d:\n%s", got, runOutputMaxItems, out.String())
	}
	if !strings.HasSuffix(out.String(), "… 3 more items (use --json for all)\n") {
		t.Errorf("no remainder line:\n%s", out.String())
	}

	big := map[string]interface{}{}
	for i := 0; i < 60; i++ {
		big[strings.Repeat("k", i+1)] = i
	}
	s.Nodes[0].OutputItems = []workflow.Item{{JSON: big}, {JSON: big}}
	out.Reset()
	printRunOutput(&out, s, runOutputWorkflow())
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	// A blank line and the heading, 40 lines of JSON, the remainder line.
	if len(lines) != 2+runOutputMaxLines+1 || !strings.HasSuffix(out.String(), "… 1 more items (use --json for all)\n") {
		t.Errorf("%d lines:\n%s", len(lines), out.String())
	}
}

func TestPrintRunOutputPrintsNothingWithoutOutput(t *testing.T) {
	for name, s := range map[string]executionSummaryJSON{
		"failed":       {Status: "FAILED", Nodes: []executionNodeJSON{{NodeID: "k", Type: "core.set", Status: "SUCCESS", OutputItems: items(1)}}},
		"waiting":      {Status: "WAITING", Nodes: []executionNodeJSON{{NodeID: "k", Type: "core.set", Status: "SUCCESS", OutputItems: items(1)}}},
		"no items":     {Status: "SUCCESS", Nodes: []executionNodeJSON{{NodeID: "k", Type: "core.set", Status: "SUCCESS"}}},
		"leaf failed":  {Status: "SUCCESS_WITH_ERRORS", Nodes: []executionNodeJSON{{NodeID: "k", Type: "core.set", Status: "FAILED"}}},
		"only trigger": {Status: "SUCCESS", Nodes: []executionNodeJSON{{NodeID: "t", Type: "trigger.manual", Status: "SUCCESS", OutputItems: items(1)}}},
	} {
		var out bytes.Buffer
		printRunOutput(&out, s, runOutputWorkflow())
		if out.Len() != 0 {
			t.Errorf("%s: printed %q", name, out.String())
		}
	}
}
