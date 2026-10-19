package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// endRun ends the first QUEUED execution with status and errMsg, after one
// node that output the item {"posted": true}. A WAITING run waits for a
// review (hil_pending "hil-1").
func endRun(t *testing.T, db *storage.Database, status, errMsg string) {
	go func() {
		for i := 0; i < 500; i++ {
			var id string
			if db.DB.QueryRow(`SELECT id FROM workflow_executions WHERE status = 'QUEUED' LIMIT 1`).Scan(&id) == nil {
				now := time.Now().UTC()
				_ = workflow.NewSQLiteWorkflowStore(db.DB).CreateExecutionNode(context.Background(), &workflow.WorkflowExecutionNode{
					ExecutionID: id, NodeID: "n1", NodeName: "post", Status: "SUCCESS",
					OutputItems: []workflow.Item{{JSON: map[string]interface{}{"posted": true}}}, StartedAt: &now, FinishedAt: &now})
				if status == "WAITING" {
					_, _ = db.DB.Exec(`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, profile_id) VALUES ('hil-1', ?, 'wf-pub', 'n', 'Review post', 'pending', 'default')`, id)
				}
				_, _ = db.DB.Exec(`UPDATE workflow_executions SET status = ?, error_message = ? WHERE id = ?`, status, errMsg, id)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

// A run that ended with tolerated node failures, or that a reviewer
// rejected, is over: the role gets it at once (with its output, or why it
// ended), not "still running" when the tool's wait runs out. So are the
// other final states and a pause for review. automation_status shows the
// same view.
func TestGrantModeReturnsEveryFinalStatusAtOnce(t *testing.T) {
	for _, c := range []struct {
		status, errMsg, output string
		review                 bool
	}{
		{"SUCCESS_WITH_ERRORS", "node notify failed: rate limited", `[{"posted":true}]`, false},
		{workflow.StatusRejected, "rejected by the reviewer", "", false},
		{"FAILED", "node post failed: 500", "", false},
		{"CANCELLED", "cancelled by the operator", "", false},
		{"WAITING", "", "", true},
	} {
		t.Run(c.status, func(t *testing.T) {
			f := newGrantFixture(t, orggrant.Tool{Wait: true, Timeout: 3})
			liveHeartbeat(t)
			endRun(t, f.db, c.status, c.errMsg)
			start := time.Now()
			out, err := f.server.callGrantTool(context.Background(), "automation_publish", nil)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "still running") || time.Since(start) > 2500*time.Millisecond {
				t.Fatalf("after %v the run is not final: %s", time.Since(start), out)
			}
			view := checkFinalView(t, out, c.status, c.errMsg, c.output, c.review)
			args, _ := json.Marshal(map[string]interface{}{"execution_id": view["execution_id"]})
			out, err = f.server.callGrantTool(context.Background(), "automation_status", args)
			if err != nil {
				t.Fatal(err)
			}
			checkFinalView(t, out, c.status, c.errMsg, c.output, c.review)
		})
	}
}

// checkFinalView checks a grant tool's view of a run that ended with status
// and errMsg, after output (when not ""), or that waits for review.
func checkFinalView(t *testing.T, out, status, errMsg, output string, review bool) map[string]interface{} {
	t.Helper()
	var view map[string]interface{}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	hil, _ := view["hil"].(map[string]interface{})
	if view["status"] != strings.ToLower(status) || (errMsg != "" && view["error"] != errMsg) ||
		(output != "" && view["output"] != output) || review != (hil != nil && hil["id"] == "hil-1") {
		t.Errorf("result: %s", out)
	}
	return view
}
