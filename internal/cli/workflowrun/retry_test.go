package workflowrun

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// QA-A U3: `workflow retry <execution-id>` runs a finished execution again
// with its trigger data and prints the new record.
func TestRetryRunsExecutionAgain(t *testing.T) {
	cfg := runDB(t, true)
	out, err := runWorkflowCmdIn(t, cfg, "", "run", "w", "--input", `{"who":"Ada"}`)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	var first executionSummaryJSON
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}

	out, err = runWorkflowCmdIn(t, cfg, "", "retry", first.ExecutionID)
	if err != nil {
		t.Fatalf("retry: %v\n%s", err, out)
	}
	var again executionSummaryJSON
	if err := json.Unmarshal([]byte(out), &again); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if again.ExecutionID == "" || again.ExecutionID == first.ExecutionID || again.Status != "SUCCESS" || again.WorkflowID != "w" {
		t.Fatalf("retry record = %+v", again)
	}
	if len(again.Nodes) != 2 || len(again.Nodes[1].OutputItems) != 1 {
		t.Fatalf("retry nodes = %+v", again.Nodes)
	}
	if got := again.Nodes[1].OutputItems[0].JSON["greeting"]; got != "hello Ada" {
		t.Errorf("greeting = %v, want the original trigger data replayed", got)
	}
}

// An execution that has not finished is refused (exit 3), and an unknown one
// is not found (exit 2).
func TestRetryRefusesUnfinishedAndUnknown(t *testing.T) {
	cfg := runDB(t, false)
	db, err := openTestDB(cfg)
	if err != nil {
		t.Fatal(err)
	}
	exec := &workflow.WorkflowExecution{WorkflowID: "w", ProfileID: "default", Status: "WAITING", TriggerType: "manual",
		TriggerData: map[string]interface{}{}}
	if err := workflow.NewSQLiteWorkflowStore(db.DB).CreateExecution(context.Background(), exec); err != nil {
		t.Fatal(err)
	}
	db.Close()

	_, err = runWorkflowCmdIn(t, cfg, "", "retry", exec.ID)
	if exitCode(err) != 3 || !strings.Contains(err.Error(), "workflow cancel") {
		t.Fatalf("retry of a WAITING run: exit %d, err %v; want exit 3 naming workflow cancel", exitCode(err), err)
	}
	_, err = runWorkflowCmdIn(t, cfg, "", "retry", "no-such-execution")
	if exitCode(err) != 2 {
		t.Fatalf("retry of an unknown id: exit %d, err %v; want 2", exitCode(err), err)
	}
}
