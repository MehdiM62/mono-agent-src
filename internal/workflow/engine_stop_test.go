package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// killedCommandNode is system.execute_command in miniature: it runs for d,
// and a cancel kills its process, which it reports on the "error" output
// rather than as an error.
type killedCommandNode struct{ d time.Duration }

func (killedCommandNode) Type() string { return "test.command" }
func (n killedCommandNode) Execute(ctx context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	select {
	case <-ctx.Done():
		return []NodeOutput{{Handle: "error", Items: []Item{NewItem(map[string]interface{}{"exitCode": -1, "error": "signal: killed"})}}}, nil
	case <-time.After(n.d):
	}
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// startStopEngine starts an engine with a stop grace under sigCtx (the
// daemon's signal context) and one RUNNING run of a test.command taking d.
func startStopEngine(t *testing.T, sigCtx context.Context, grace, d time.Duration) (*WorkflowEngine, *SQLiteWorkflowStore, string) {
	t.Helper()
	store := newFullEngineStore(t)
	reg := NewNodeTypeRegistry()
	reg.Register("test.command", func() NodeExecutor { return killedCommandNode{d: d} })
	eng := NewWorkflowEngineWithStore(store, store.RawDB(), &fakeScheduler{}, reg,
		EngineConfig{ProfileID: "default", WebhookAddr: "127.0.0.1:0"}, zerolog.Nop())
	eng.SetStopGrace(grace)
	t.Cleanup(func() { _ = eng.Stop() })
	if err := eng.Start(sigCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	wf := createActiveManualWorkflow(t, context.Background(), eng, "test.command")
	id, err := eng.TriggerWorkflow(context.Background(), wf, nil)
	if err != nil {
		t.Fatalf("TriggerWorkflow: %v", err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if st, _ := store.GetExecutionStatus(context.Background(), id); st == "RUNNING" {
			return eng, store, id
		}
		if time.Now().After(deadline) {
			t.Fatal("the run did not start")
		}
	}
}

// A run the daemon's stop interrupts ends CANCELLED and says the daemon
// stopped, even when its node reports the kill on its error output, and the
// signal alone does not cut it short before the grace.
func TestStopCancelsRunInFlightAsDaemonStopping(t *testing.T) {
	sigCtx, signal := context.WithCancel(context.Background())
	eng, store, id := startStopEngine(t, sigCtx, 300*time.Millisecond, time.Minute)

	signal()
	time.Sleep(150 * time.Millisecond)
	if st, _ := store.GetExecutionStatus(context.Background(), id); st != "RUNNING" {
		t.Fatalf("status after the signal = %s, want RUNNING until Stop's grace ends", st)
	}
	_ = eng.Stop()

	exec, err := store.GetExecution(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if exec.Status != "CANCELLED" || !strings.Contains(exec.ErrorMessage, "daemon stopping") {
		t.Fatalf("run = %s %q, want CANCELLED with daemon stopping", exec.Status, exec.ErrorMessage)
	}
	for _, n := range exec.Nodes {
		if n.NodeID == "n" && (n.Status != "FAILED" || !strings.Contains(n.ErrorMessage, "daemon stopping")) {
			t.Fatalf("command node = %s %q, want FAILED with daemon stopping", n.Status, n.ErrorMessage)
		}
	}
}

// A run that ends within the stop grace finishes normally.
func TestStopGraceLetsRunFinish(t *testing.T) {
	eng, store, id := startStopEngine(t, context.Background(), 10*time.Second, 300*time.Millisecond)

	_ = eng.Stop()

	if st, _ := store.GetExecutionStatus(context.Background(), id); st != "SUCCESS" {
		t.Fatalf("status = %s, want SUCCESS within the grace", st)
	}
}

// Recovering a run its dead process left RUNNING also closes the node rows
// it was in the middle of.
func TestRecoverStaleExecutionsClosesRunningNodes(t *testing.T) {
	store := newFullEngineStore(t)
	pid := deadPID(t)
	if _, err := store.db.Exec(`INSERT INTO workflow_executions (id, workflow_id, status, trigger_type, pid) VALUES ('e1','wf','RUNNING','trigger.manual',?);
		INSERT INTO workflow_execution_nodes (id, execution_id, node_id, node_name, status, error_message) VALUES
		('n1','e1','a','A','SUCCESS',''), ('n2','e1','b','B','RUNNING','')`, pid); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverStaleExecutions(context.Background()); err != nil {
		t.Fatal(err)
	}
	exec, err := store.GetExecution(context.Background(), "e1")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range exec.Nodes {
		got[n.NodeID] = n.Status + " " + n.ErrorMessage
	}
	if exec.Status != "FAILED" || got["a"] != "SUCCESS " || got["b"] != "FAILED interrupted: process restart" {
		t.Fatalf("run %s, nodes %v", exec.Status, got)
	}
}
