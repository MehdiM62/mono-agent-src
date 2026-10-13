package workflow

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// twoTriggerWorkflow: t1 → n1 (counts a), t2 → n2 (counts b).
func twoTriggerWorkflow(t *testing.T, ctx context.Context, eng *WorkflowEngine, a, b *int32) string {
	t.Helper()
	eng.registry.Register("test.count.a", func() NodeExecutor { return atomicCountNode{runs: a} })
	eng.registry.Register("test.count.b", func() NodeExecutor { return atomicCountNode{runs: b} })
	wf := &Workflow{
		ID: "wf-two", Name: "wf-two", ProfileID: "default",
		Nodes: []WorkflowNode{
			{ID: "t1", Type: "trigger.manual", Name: "T1"},
			{ID: "t2", Type: "trigger.manual", Name: "T2"},
			{ID: "n1", Type: "test.count.a", Name: "N1"},
			{ID: "n2", Type: "test.count.b", Name: "N2"},
		},
		Connections: []WorkflowConnection{
			{SourceNodeID: "t1", SourceHandle: "main", TargetNodeID: "n1", TargetHandle: "main"},
			{SourceNodeID: "t2", SourceHandle: "main", TargetNodeID: "n2", TargetHandle: "main"},
		},
	}
	for i := range wf.Nodes {
		wf.Nodes[i].WorkflowID = wf.ID
	}
	if err := eng.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	if err := eng.store.SetWorkflowActive(ctx, wf.ID, true); err != nil {
		t.Fatal(err)
	}
	return wf.ID
}

// TestTriggerNodeID_PersistedThroughAdoptAndRetry is #10: the fired trigger
// node survives on the row, so an adopted run and its retry only run that
// trigger's branch.
func TestTriggerNodeID_PersistedThroughAdoptAndRetry(t *testing.T) {
	store := newFullEngineStore(t)
	eng := newTestEngine(t, store, NewNodeTypeRegistry())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var a, b int32
	wfID := twoTriggerWorkflow(t, ctx, eng, &a, &b)
	eng.queue.Start(ctx)

	// An unowned row (as --no-wait or a dead owner leaves) that t2 fired.
	row := &WorkflowExecution{WorkflowID: wfID, Status: "QUEUED", TriggerType: "trigger.manual", TriggerNodeID: "t2"}
	if err := store.CreateExecution(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetExecution(ctx, row.ID)
	if err != nil || got.TriggerNodeID != "t2" {
		t.Fatalf("GetExecution trigger_node_id = %q, %v; want t2", got.TriggerNodeID, err)
	}
	eng.adoptQueuedExecutions(ctx)
	if st := waitForTerminalStatus(t, store, row.ID, 10*time.Second); st != "SUCCESS" {
		t.Fatalf("adopted run = %s", st)
	}
	if atomic.LoadInt32(&a) != 0 || atomic.LoadInt32(&b) != 1 {
		t.Fatalf("adopted run branches a=%d b=%d, want 0/1", a, b)
	}

	retry, err := eng.RetryExecution(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st := waitForTerminalStatus(t, store, retry, 10*time.Second); st != "SUCCESS" {
		t.Fatalf("retry = %s", st)
	}
	if atomic.LoadInt32(&a) != 0 || atomic.LoadInt32(&b) != 2 {
		t.Fatalf("after retry a=%d b=%d, want 0/2", a, b)
	}
	if r, _ := store.GetExecution(ctx, retry); r == nil || r.TriggerNodeID != "t2" {
		t.Errorf("retry row trigger_node_id = %+v, want t2", r)
	}
}

// TestTriggerNodeID_HandleTriggerPersists: a trigger fire stores the node.
func TestTriggerNodeID_HandleTriggerPersists(t *testing.T) {
	store := newFullEngineStore(t)
	eng := newTestEngine(t, store, NewNodeTypeRegistry())
	ctx := context.Background()
	var a, b int32
	wfID := twoTriggerWorkflow(t, ctx, eng, &a, &b)
	eng.ctx = ctx
	eng.handleTrigger(wfID, "t1", nil)
	var node string
	if err := store.RawDB().QueryRow(`SELECT trigger_node_id FROM workflow_executions WHERE workflow_id=?`, wfID).Scan(&node); err != nil {
		t.Fatal(err)
	}
	if node != "t1" {
		t.Fatalf("trigger_node_id = %q, want t1", node)
	}
}
