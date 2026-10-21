package workflow

import (
	"context"
	"testing"
)

// pollThenRecord answers a status poll with poll and a read of the record
// with record: an engine resuming or finishing the run between the two reads.
type pollThenRecord struct {
	*SQLiteWorkflowStore
	poll   string
	record *WorkflowExecution
	read   bool
}

func (s *pollThenRecord) GetExecutionStatus(context.Context, string) (string, error) {
	return s.poll, nil
}

func (s *pollThenRecord) GetExecution(context.Context, string) (*WorkflowExecution, error) {
	s.read = true
	return s.record, nil
}

// A WAITING poll is decided on the record read after it: a parent the engine
// resumed in between is in flight, one that ended is not, and its status is
// the record's. A poll that is not WAITING decides alone.
func TestExecutionInFlightDecidesOnTheRecordItReads(t *testing.T) {
	for _, c := range []struct {
		poll, record, status string
		inFlight, read       bool
	}{
		{"WAITING", "RUNNING", "RUNNING", true, true},
		{"WAITING", "QUEUED", "QUEUED", true, true},
		{"WAITING", "SUCCESS", "SUCCESS", false, true},
		{"WAITING", "WAITING", "WAITING", false, true}, // no database: no children to wait on
		{"RUNNING", "", "RUNNING", true, false},
		{"FAILED", "", "FAILED", false, false},
	} {
		store := &pollThenRecord{SQLiteWorkflowStore: NewSQLiteWorkflowStore(nil), poll: c.poll,
			record: &WorkflowExecution{ID: "p", Status: c.record}}
		status, inFlight, err := ExecutionInFlight(context.Background(), store, "p")
		if err != nil || status != c.status || inFlight != c.inFlight || store.read != c.read {
			t.Errorf("poll %s, record %s: got %s, %v (record read %v, %v), want %s, %v (read %v)",
				c.poll, c.record, status, inFlight, store.read, err, c.status, c.inFlight, c.read)
		}
	}
}
