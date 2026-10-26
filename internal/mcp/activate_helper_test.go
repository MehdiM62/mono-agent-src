package mcp

import (
	"context"
	"testing"
)

// activateForTest switches a saved workflow on the way the operator does: a workflow an agent
// saves through a tool starts inactive.
func activateForTest(t *testing.T, s *Server, id string) {
	t.Helper()
	rt, err := s.runtime()
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.store.SetWorkflowActive(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
}
