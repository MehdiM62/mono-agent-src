package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A node whose type is not installed leaves a FAILED row naming it, so the
// run's detail shows which node failed instead of only the run's error.
func TestRunExecution_UnknownNodeTypeRecordsFailedRow(t *testing.T) {
	wf := &workflow.Workflow{ID: "w", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "T"},
		{ID: "x", Type: "nope.missing", Name: "Missing"},
	}, Connections: []workflow.WorkflowConnection{conn("t", "main", "x")}}
	store := &failedRowStore{}
	err := runWF(t, workflow.NewNodeTypeRegistry(), wf, store, "")
	if !errors.Is(err, workflow.ErrNodeTypeUnknown) {
		t.Fatalf("run err = %v, want ErrNodeTypeUnknown", err)
	}
	if st := store.statuses("x"); len(st) != 1 {
		t.Fatalf("rows for x = %v, want one", st)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.finished) != 1 || store.finished[0].status != "FAILED" || store.finished[0].errMsg != err.Error() {
		t.Fatalf("finished = %+v, want one FAILED with %q", store.finished, err)
	}
}

// failedRowStore records both the created rows and how they finished.
type failedRowStore struct {
	recStore
	finished []finishedRow
}

func (s *failedRowStore) SetExecutionNodeFinished(_ context.Context, _ string, status string, items []workflow.Item, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished = append(s.finished, finishedRow{status, items, errMsg})
	return nil
}
