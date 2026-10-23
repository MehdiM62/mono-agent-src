package main

import (
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
)

// The binary hands the workflow-generate kind the real node registry, so an
// invented node type in a generated workflow is refused.
func TestWorkflowGenerateKindKnowsTheRealNodeTypes(t *testing.T) {
	known := aitask.WorkflowNodeTypeKnown
	if known == nil {
		t.Fatal("the node registry is not wired to the workflow-generate kind")
	}
	if !known("http.request") || known("http.nope") {
		t.Fatalf("http.request=%v http.nope=%v", known("http.request"), known("http.nope"))
	}
}
