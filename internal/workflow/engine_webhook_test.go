package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/account/accounttest"
)

func TestHandleWebhookTrigger_ReportsWhyNoRun(t *testing.T) {
	eng := newTriggerEngine(&stubStore{})
	if _, err := eng.handleWebhookTrigger("wf-x", "t1", nil, nil); !errors.Is(err, ErrWebhookWorkflowNotFound) {
		t.Errorf("missing workflow: err = %v, want ErrWebhookWorkflowNotFound", err)
	}

	store := &stubStore{workflowToReturn: &Workflow{ID: "wf-x", Nodes: []WorkflowNode{{ID: "t1", Type: "trigger.webhook"}}}}
	eng = newTriggerEngine(store)
	if _, err := eng.handleWebhookTrigger("wf-x", "t1", nil, nil); !errors.Is(err, ErrWebhookWorkflowInactive) {
		t.Errorf("inactive workflow: err = %v, want ErrWebhookWorkflowInactive", err)
	}
	if len(store.createdExecs) != 0 {
		t.Errorf("created %d executions for an inactive workflow", len(store.createdExecs))
	}
}

func TestHandleWebhookTrigger_LockedAccount(t *testing.T) {
	accounttest.Install(t, accounttest.LockedNoLogin)
	store := newEngineGateStore(engineGateWorkflow())
	eng := newTriggerEngine(store)
	if _, err := eng.handleWebhookTrigger("wf-gate", "t1", nil, nil); !account.IsLoginRequired(err) {
		t.Fatalf("err = %v, want login required", err)
	}
	if len(store.createdExecs) != 0 {
		t.Fatalf("a locked account created %d executions", len(store.createdExecs))
	}
}

// prepare runs with the execution ID before the run is queued.
func TestHandleWebhookTrigger_PrepareBeforeQueue(t *testing.T) {
	store := &stubStore{workflowToReturn: &Workflow{ID: "wf-x", IsActive: true, ProfileID: "p",
		Nodes: []WorkflowNode{{ID: "t1", Type: "trigger.webhook"}}}}
	eng := newTriggerEngine(store)
	var prepared string
	id, err := eng.handleWebhookTrigger("wf-x", "t1", []Item{{JSON: map[string]interface{}{"a": 1}}}, func(execID string) { prepared = execID })
	if err != nil {
		t.Fatal(err)
	}
	if prepared != id {
		t.Errorf("prepare got %q, run is %q", prepared, id)
	}
	if store.createdExecs[0].TriggerType != "trigger.webhook" {
		t.Errorf("trigger type = %q", store.createdExecs[0].TriggerType)
	}
}

// echoNode answers with {"echo": <name>} and, as a Respond node would, sends
// that to a waiting webhook request.
type echoNode struct{ respond bool }

func (echoNode) Type() string { return "test.echo" }
func (n echoNode) Execute(_ context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	out := Item{JSON: map[string]interface{}{"echo": in.Items[0].JSON["name"]}}
	if n.respond {
		DeliverWebhookResponse(in.ExecutionID, WebhookResponse{StatusCode: http.StatusCreated, Body: []byte("echo:" + out.JSON["echo"].(string))})
	}
	return []NodeOutput{{Handle: "main", Items: []Item{out}}}, nil
}

// webhookEngine runs an engine over a migrated database with one active
// webhook workflow trigger.webhook -> test.echo. No port is bound: requests
// go straight to the server's handler.
func webhookEngine(t *testing.T, respond bool, config map[string]interface{}) *WorkflowEngine {
	t.Helper()
	old := webhookPollInterval
	webhookPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { webhookPollInterval = old })

	store, _ := newMigratedStore(t)
	reg := NewNodeTypeRegistry()
	reg.Register("test.echo", func() NodeExecutor { return echoNode{respond: respond} })
	eng := NewWorkflowEngineWithStore(store, store.RawDB(), nil, reg, EngineConfig{ProfileID: "default"}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = eng.Stop() })
	eng.queue.Start(ctx)

	config["path"] = "it"
	wf := &Workflow{
		ID: "wf-hook", Name: "wf-hook", ProfileID: "default",
		Nodes: []WorkflowNode{
			{ID: "t", WorkflowID: "wf-hook", Type: "trigger.webhook", Name: "T", Config: config},
			{ID: "n", WorkflowID: "wf-hook", Type: "test.echo", Name: "N"},
		},
		Connections: []WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "n", TargetHandle: "main"}},
	}
	if err := eng.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	if err := eng.ActivateWorkflow(ctx, wf.ID); err != nil {
		t.Fatal(err)
	}
	return eng
}

func hit(eng *WorkflowEngine, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhook/it", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	eng.webhookServer.ServeHTTP(rec, req)
	return rec
}

func TestEngineWebhook_OnReceivedRunsWithBodyFields(t *testing.T) {
	eng := webhookEngine(t, false, map[string]interface{}{})
	rec := hit(eng, `{"name":"ada"}`)
	var m map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	execID, _ := m["execution_id"].(string)
	if rec.Code != http.StatusOK || execID == "" {
		t.Fatalf("got %d %s, want 200 with an execution_id", rec.Code, rec.Body.String())
	}
	if st := waitForTerminalStatus(t, eng.store.(*SQLiteWorkflowStore), execID, 10*time.Second); st != "SUCCESS" {
		t.Fatalf("run = %s, want SUCCESS", st)
	}
}

func TestEngineWebhook_LastNode(t *testing.T) {
	eng := webhookEngine(t, false, map[string]interface{}{"response_mode": "lastNode"})
	rec := hit(eng, `{"name":"ada"}`)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"echo":"ada"}` {
		t.Fatalf("got %d %s, want 200 {\"echo\":\"ada\"}", rec.Code, rec.Body.String())
	}
}

func TestEngineWebhook_ResponseNode(t *testing.T) {
	eng := webhookEngine(t, true, map[string]interface{}{"response_mode": "responseNode"})
	rec := hit(eng, `{"name":"bo"}`)
	if rec.Code != http.StatusCreated || rec.Body.String() != "echo:bo" {
		t.Fatalf("got %d %q, want 201 echo:bo", rec.Code, rec.Body.String())
	}
}

func TestEngineWebhook_DedupeAcrossRequests(t *testing.T) {
	eng := webhookEngine(t, false, map[string]interface{}{"dedupe_header": "X-GitHub-Delivery"})
	first := hit(eng, `{"name":"a"}`, "X-GitHub-Delivery", "abc")
	dup := hit(eng, `{"name":"a"}`, "X-GitHub-Delivery", "abc")
	var f, d map[string]interface{}
	_ = json.Unmarshal(first.Body.Bytes(), &f)
	_ = json.Unmarshal(dup.Body.Bytes(), &d)
	if d["duplicate"] != true || d["execution_id"] != f["execution_id"] {
		t.Fatalf("duplicate = %v, first = %v", d, f)
	}
	execs, err := eng.store.ListExecutions(context.Background(), "wf-hook", 10)
	if err != nil || len(execs) != 1 {
		t.Fatalf("executions = %d (%v), want 1", len(execs), err)
	}
}

// An inactive workflow's webhook is answered 409, not 200.
func TestEngineWebhook_InactiveIsNot200(t *testing.T) {
	eng := webhookEngine(t, false, map[string]interface{}{})
	if err := eng.store.SetWorkflowActive(context.Background(), "wf-hook", false); err != nil {
		t.Fatal(err)
	}
	if rec := hit(eng, `{}`); rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}
