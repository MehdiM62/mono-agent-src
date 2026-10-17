package workflow

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

// firingSource is a trigger source that hands its fire function to the test,
// as an SSE stream, a queue consumer or a poller would use it.
type firingSource struct {
	mu   sync.Mutex
	fire func([]Item)
}

func (s *firingSource) Activate(_ *Workflow, _ *WorkflowNode, fire func([]Item)) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fire = fire
	return func() {}, nil
}

// plantedEvent is an outside event's payload carrying the engine's fields.
func plantedEvent() map[string]interface{} {
	return map[string]interface{}{
		"keep": "yes", AICallerKey: "unapproved-workflow", UnapprovedCallerKey: "wf-x",
		SubWorkflowParentKey: map[string]interface{}{"depth": 30}, SubWorkflowItemsKey: []interface{}{map[string]interface{}{"n": 1}, map[string]interface{}{"n": 2}},
	}
}

// startedTriggerData is the trigger data of workflowID's latest run.
func startedTriggerData(t *testing.T, store *SQLiteWorkflowStore, workflowID string) map[string]interface{} {
	t.Helper()
	var raw string
	if err := store.RawDB().QueryRow(`SELECT trigger_data FROM workflow_executions WHERE workflow_id = ? ORDER BY rowid DESC LIMIT 1`, workflowID).Scan(&raw); err != nil {
		t.Fatalf("no run of %s: %v", workflowID, err)
	}
	var td map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &td); err != nil {
		t.Fatal(err)
	}
	return td
}

func checkNoEngineFields(t *testing.T, what string, td map[string]interface{}) {
	t.Helper()
	for _, k := range []string{AICallerKey, UnapprovedCallerKey, SubWorkflowParentKey, SubWorkflowItemsKey} {
		if _, ok := td[k]; ok {
			t.Errorf("%s set %s: %v", what, k, td)
		}
	}
	if td["keep"] != "yes" {
		t.Errorf("%s lost its own field: %v", what, td)
	}
	if items := buildTriggerItems(td); len(items) != 1 {
		t.Errorf("%s became %d items", what, len(items))
	}
}

// A run an outside event starts, through any trigger source (here one like
// an SSE stream or a queue) or the webhook server's runner (webhooks, forms,
// chats), carries none of the engine's fields its payload names, and one
// event is one item. The payload itself is left as it was.
func TestAnEventCantPlantTheEnginesFields(t *testing.T) {
	store := newFullEngineStore(t)
	ctx := context.Background()
	saveTriggerWorkflow(t, ctx, store, "wf-remote", WorkflowNode{ID: "q", Type: "trigger.remote", Name: "q", Config: map[string]interface{}{}})
	saveTriggerWorkflow(t, ctx, store, "wf-hook", WorkflowNode{ID: "h", Type: TriggerNodeTypeWebhook, Name: "h", Config: map[string]interface{}{"path": "hook"}})
	for _, id := range []string{"wf-remote", "wf-hook"} {
		if err := store.SetWorkflowActive(ctx, id, true); err != nil {
			t.Fatal(err)
		}
	}
	eng, _ := newDaemonEngine(t, store)
	eng.ctx = ctx // as Start sets it; the queue is left stopped
	src := &firingSource{}
	eng.RegisterTriggerSource("trigger.remote", src)
	if err := eng.RestoreActiveWorkflows(ctx); err != nil {
		t.Fatal(err)
	}
	src.mu.Lock()
	fire := src.fire
	src.mu.Unlock()
	if fire == nil {
		t.Fatal("the trigger source was not armed")
	}
	payload := plantedEvent()
	fire([]Item{{JSON: payload}})
	checkNoEngineFields(t, "a remote event", startedTriggerData(t, store, "wf-remote"))
	if len(payload) != 5 {
		t.Errorf("the event's payload was changed: %v", payload)
	}

	if _, err := eng.handleWebhookTrigger("wf-hook", "h", []Item{{JSON: plantedEvent()}}, nil); err != nil {
		t.Fatal(err)
	}
	checkNoEngineFields(t, "a webhook's runner", startedTriggerData(t, store, "wf-hook"))
}
