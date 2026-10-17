package mcp

import (
	"context"
	"encoding/json"
	"fmt"
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

// workflow_status returns the run's data as well as its waiting list: a
// reply's token in a node's input or output items or error, in the run's
// error or in its trigger data stays out of it, in any form the redactor
// knows (here the link, a slice, hex, the reversal and a spaced split).
func TestWorkflowStatusHidesReplyTokensInRunData(t *testing.T) {
	t.Setenv("MONOAGENT_PUBLIC_URL", "http://127.0.0.1:19728")
	s := newTestServer(t)
	rt, err := s.runtime()
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	ctx := context.Background()
	if err := rt.store.CreateWorkflow(ctx, &workflow.Workflow{ID: "wf-data", Name: "Data", ProfileID: "default"}); err != nil {
		t.Fatal(err)
	}
	exec := &workflow.WorkflowExecution{ID: "exec-data", WorkflowID: "wf-data", ProfileID: "default", Status: "WAITING", TriggerType: "manual"}
	if err := rt.store.CreateExecution(ctx, exec); err != nil {
		t.Fatal(err)
	}
	ask := strings.Repeat("A", 20) + strings.Repeat("b", 23)
	resume, err := workflow.ExecutionResumeURL(exec.ID)
	if err != nil {
		t.Fatal(err)
	}
	tok := resume[strings.LastIndex(resume, "/")+1:]
	if _, err := rt.db.DB.Exec(`INSERT INTO wait_replies (execution_id, node_id, run_index, workflow_id, token, kind, spec)
		VALUES ('exec-data','ask',0,'wf-data',?,'approval','{"kind":"approval","outbound":true}')`, ask); err != nil {
		t.Fatal(err)
	}
	spaced := strings.Join(strings.Split(tok, ""), " ")
	if _, err := rt.db.DB.Exec(`UPDATE workflow_executions SET trigger_data = ?, error_message = ? WHERE id = 'exec-data'`,
		`{"chars":"`+spaced+`"}`, "gave up on "+reverse(tok)); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.db.DB.Exec(`INSERT INTO workflow_execution_nodes (id, execution_id, node_id, node_name, status, input_items, output_items, error_message, error_hint, started_at)
		VALUES ('en1','exec-data','set','Keep','FAILED',?,?,?,?,'2026-10-17 09:00:00')`,
		`[{"json":{"head":"`+resume[:50]+`"}}]`, `[{"json":{"approve":"http://127.0.0.1:19728/reply/`+ask+`/approve"}}]`,
		fmt.Sprintf("posting %x failed", tok), "retry with "+ask[30:]); err != nil {
		t.Fatal(err)
	}

	resps := serveLines(t, s, callToolReq(1, "workflow_status", map[string]interface{}{"execution_id": exec.ID}))
	text, isErr := toolText(t, respByID(t, resps, "1"))
	if isErr {
		t.Fatalf("workflow_status: %s", text)
	}
	for _, form := range []string{tok[:12], tok[30:], ask, ask[30:], fmt.Sprintf("%x", tok)[:20], reverse(tok)[:12], spaced[:24]} {
		if strings.Contains(text, form) {
			t.Errorf("workflow_status has %q:\n%s", form, text)
		}
	}
	if strings.Count(text, workflow.ReplyTokenHidden) < 6 {
		t.Errorf("want every place hidden:\n%s", text)
	}
}

func reverse(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
