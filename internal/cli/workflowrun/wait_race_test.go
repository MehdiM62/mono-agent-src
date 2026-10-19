package workflowrun

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/workflow"
)

// resumingStore answers for a parent the engine resumes while the wait looks
// at it: each poll and each read of the record takes the next answer of its
// list (the last one repeats).
type resumingStore struct {
	*workflow.SQLiteWorkflowStore
	polls, records []string
	polled, read   int
}

func nth(list []string, i int) string {
	if i < len(list) {
		return list[i]
	}
	return list[len(list)-1]
}

func (s *resumingStore) GetExecutionStatus(context.Context, string) (string, error) {
	s.polled++
	return nth(s.polls, s.polled-1), nil
}

func (s *resumingStore) GetExecution(_ context.Context, id string) (*workflow.WorkflowExecution, error) {
	s.read++
	return &workflow.WorkflowExecution{ID: id, Status: nth(s.records, s.read-1)}, nil
}

// The poll reads the parent WAITING for its sub-workflow, and the engine
// resumes it before the record is read: that read says RUNNING. The run is
// still in flight, so the wait polls again and ends only on SUCCESS; it used
// to take the RUNNING parent for one paused for a person and return.
func TestWaitForExecutionKeepsWaitingForAParentResumedMidPoll(t *testing.T) {
	// No database: the two lists are all the wait reads.
	store := &resumingStore{
		SQLiteWorkflowStore: workflow.NewSQLiteWorkflowStore(nil),
		polls:               []string{"WAITING", "SUCCESS"},
		records:             []string{"RUNNING", "SUCCESS"},
	}
	engine := workflow.NewWorkflowEngineWithStore(store, nil, nil, workflow.NewNodeTypeRegistry(),
		workflow.EngineConfig{ProfileID: "default", WebhookAddr: "127.0.0.1:0", MaxConcurrent: 1}, zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := waitForExecution(ctx, engine, "parent", 15*time.Second, true, nil); err != nil {
		t.Fatal(err)
	}
	if store.polled != 2 {
		t.Fatalf("the wait polled %d times: it returned while the parent was RUNNING, want it to poll on to SUCCESS", store.polled)
	}
}

// What the wait reports, and its exit, are the status the in-flight check
// decided on. The record is read once more only for its error message, and a
// change in between (a person approving a paused run) does not turn a pause
// or a failure into a "RUNNING" that exits 0.
func TestWaitForExecutionReportsTheStatusItDecidedOn(t *testing.T) {
	for _, c := range []struct {
		polls, records []string
		printed        string
		fails          bool
	}{
		{[]string{"WAITING"}, []string{"WAITING", "RUNNING"}, "Status: WAITING\nHint:   " + waitingHint + "\n", false},
		{[]string{"FAILED"}, []string{"RUNNING"}, "Status: FAILED\n", true},
	} {
		store := &resumingStore{SQLiteWorkflowStore: workflow.NewSQLiteWorkflowStore(nil), polls: c.polls, records: c.records}
		engine := workflow.NewWorkflowEngineWithStore(store, nil, nil, workflow.NewNodeTypeRegistry(),
			workflow.EngineConfig{ProfileID: "default", WebhookAddr: "127.0.0.1:0", MaxConcurrent: 1}, zerolog.Nop())
		var err error
		out := captureStdout(t, func() {
			err = waitForExecution(context.Background(), engine, "run", 15*time.Second, false, nil)
		})
		if out != c.printed || (err != nil) != c.fails {
			t.Errorf("poll %v, records %v: printed %q, err %v; want %q, fails %v", c.polls, c.records, out, err, c.printed, c.fails)
		}
	}
}
