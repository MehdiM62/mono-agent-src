package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// An agent reaches this server: workflow_status leaves out the links of a
// reply that lets a message to a person go out, and keeps a plain one's.
func TestWorkflowStatusHidesARepliesLinks(t *testing.T) {
	t.Setenv("MONOAGENT_PUBLIC_URL", "http://127.0.0.1:19721")
	s := newTestServer(t)
	rt, err := s.runtime()
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	ctx := context.Background()
	wf := &workflow.Workflow{ID: "wf-wait", Name: "Wait", ProfileID: "default"}
	if err := rt.store.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	exec := &workflow.WorkflowExecution{ID: "exec-wait", WorkflowID: wf.ID, ProfileID: "default", Status: "WAITING", TriggerType: "manual"}
	if err := rt.store.CreateExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.db.DB.Exec(`INSERT INTO wait_replies (execution_id, node_id, run_index, workflow_id, token, kind, spec) VALUES
		('exec-wait','ask',0,'wf-wait','tokAsk','approval','{"kind":"approval","outbound":true}'),
		('exec-wait','wait',0,'wf-wait','tokWait','webhook','{"kind":"webhook","outbound":false}'),
		('exec-wait','old',0,'wf-wait','tokOld','approval','{"kind":"approval"}')`); err != nil {
		t.Fatal(err)
	}
	resps := serveLines(t, s, callToolReq(1, "workflow_status", map[string]interface{}{"execution_id": exec.ID}))
	text, isErr := toolText(t, respByID(t, resps, "1"))
	if isErr {
		t.Fatalf("workflow_status: %s", text)
	}
	if strings.Contains(text, "tokAsk") || strings.Contains(text, "tokOld") {
		t.Fatalf("the send's reply link reached the agent: %s", text)
	}
	var got struct {
		Waiting []workflow.PendingReply `json:"waiting_for"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}
	if len(got.Waiting) != 3 || got.Waiting[0].URL != "" || !strings.Contains(got.Waiting[0].Note, "message to a person") ||
		got.Waiting[1].URL != "http://127.0.0.1:19721/reply/tokWait" || got.Waiting[2].URL != "" || !got.Waiting[2].Outbound {
		t.Fatalf("waiting_for = %+v", got.Waiting)
	}
}
