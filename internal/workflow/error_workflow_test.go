package workflow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// failingNode always fails with err.
type failingNode struct{ err error }

func (failingNode) Type() string { return "test.fail" }
func (f failingNode) Execute(context.Context, NodeInput, map[string]interface{}) ([]NodeOutput, error) {
	return nil, f.err
}

// captureNode records the items it receives.
type captureNode struct {
	mu    *sync.Mutex
	items *[]Item
}

func (captureNode) Type() string { return "test.capture" }
func (c captureNode) Execute(_ context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	c.mu.Lock()
	*c.items = append(*c.items, in.Items...)
	c.mu.Unlock()
	return []NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

type errorWorkflowRig struct {
	eng   *WorkflowEngine
	store *SQLiteWorkflowStore
	mu    sync.Mutex
	got   []Item
}

func newErrorWorkflowRig(t *testing.T, failErr error) *errorWorkflowRig {
	t.Helper()
	r := &errorWorkflowRig{store: newFullEngineStore(t)}
	reg := NewNodeTypeRegistry()
	reg.Register("test.fail", func() NodeExecutor { return failingNode{err: failErr} })
	reg.Register("test.capture", func() NodeExecutor { return captureNode{mu: &r.mu, items: &r.got} })
	r.eng = newTestEngine(t, r.store, reg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.eng.queue.Start(ctx)
	return r
}

// addWorkflow saves trigger → node (type nodeType) and activates it when active.
func (r *errorWorkflowRig) addWorkflow(t *testing.T, id, triggerType, nodeType string, settings map[string]interface{}, active bool) {
	t.Helper()
	ctx := context.Background()
	wf := &Workflow{
		ID: id, Name: "name-" + id, ProfileID: "default", Settings: settings,
		Nodes: []WorkflowNode{
			{ID: id + "-t", WorkflowID: id, Type: triggerType, Name: "T"},
			{ID: id + "-n", WorkflowID: id, Type: nodeType, Name: "N"},
		},
		Connections: []WorkflowConnection{{ID: id + "-c", SourceNodeID: id + "-t", SourceHandle: "main", TargetNodeID: id + "-n", TargetHandle: "main"}},
	}
	if err := r.eng.CreateWorkflow(ctx, wf); err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	if active {
		if err := r.store.SetWorkflowActive(ctx, id, true); err != nil {
			t.Fatalf("activate: %v", err)
		}
	}
}

// errorRuns lists the executions of workflowID started by an error trigger.
func (r *errorWorkflowRig) errorRuns(t *testing.T, workflowID string) []WorkflowExecution {
	t.Helper()
	all, err := r.store.ListExecutions(context.Background(), workflowID, 0)
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	var out []WorkflowExecution
	for _, e := range all {
		if e.TriggerType == TriggerNodeTypeError {
			out = append(out, e)
		}
	}
	return out
}

func (r *errorWorkflowRig) waitErrorRun(t *testing.T, workflowID string) WorkflowExecution {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if runs := r.errorRuns(t, workflowID); len(runs) > 0 {
			waitForTerminalStatus(t, r.store, runs[0].ID, 30*time.Second)
			return runs[0]
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no error-trigger run of %s", workflowID)
	return WorkflowExecution{ProfileID: "default"}
}

func TestErrorWorkflowRunsWithFailurePayload(t *testing.T) {
	r := newErrorWorkflowRig(t, errors.New("linear: status 502 Bad Gateway"))
	r.addWorkflow(t, "wf-err", TriggerNodeTypeError, "test.capture", nil, false)
	r.addWorkflow(t, "wf-main", "trigger.manual", "test.fail", map[string]interface{}{ErrorWorkflowSetting: "wf-err"}, true)

	execID, err := r.eng.TriggerWorkflow(context.Background(), "wf-main", nil)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if st := waitForTerminalStatus(t, r.store, execID, 10*time.Second); st != "FAILED" {
		t.Fatalf("status = %s, want FAILED", st)
	}
	run := r.waitErrorRun(t, "wf-err")
	if st, _ := r.store.GetExecutionStatus(context.Background(), run.ID); st != "SUCCESS" {
		t.Fatalf("error workflow status = %s, want SUCCESS", st)
	}

	failed, _ := r.store.GetExecution(context.Background(), execID)
	if failed.ErrorClass != string(ClassUpstream5xx) {
		t.Errorf("execution error_class = %q", failed.ErrorClass)
	}
	var node *WorkflowExecutionNode
	for i := range failed.Nodes {
		if failed.Nodes[i].Status == "FAILED" {
			node = &failed.Nodes[i]
		}
	}
	if node == nil || node.ErrorClass != string(ClassUpstream5xx) || node.ErrorHint == "" {
		t.Fatalf("failed node record = %+v", node)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) != 1 {
		t.Fatalf("error workflow got %d items", len(r.got))
	}
	p := r.got[0].JSON
	ex, _ := p["execution"].(map[string]interface{})
	wf, _ := p["workflow"].(map[string]interface{})
	errObj, _ := ex["error"].(map[string]interface{})
	nodeObj, _ := errObj["node"].(map[string]interface{})
	switch {
	case ex["id"] != execID, ex["mode"] != "manual", ex["lastNodeExecuted"] != "N",
		ex["error_class"] != string(ClassUpstream5xx), ex["error_hint"] == "":
		t.Errorf("execution = %#v", ex)
	case wf["id"] != "wf-main", wf["name"] != "name-wf-main":
		t.Errorf("workflow = %#v", wf)
	case nodeObj["name"] != "N", nodeObj["type"] != "test.fail":
		t.Errorf("error.node = %#v", nodeObj)
	}
	if msg, _ := errObj["message"].(string); msg == "" {
		t.Errorf("error.message empty")
	}
}

// An error workflow that fails, and names itself as its error workflow,
// starts no further run.
func TestErrorWorkflowDoesNotTriggerItself(t *testing.T) {
	r := newErrorWorkflowRig(t, errors.New("boom"))
	r.addWorkflow(t, "wf-err", TriggerNodeTypeError, "test.fail", map[string]interface{}{ErrorWorkflowSetting: "wf-err"}, false)
	r.addWorkflow(t, "wf-main", "trigger.manual", "test.fail", map[string]interface{}{ErrorWorkflowSetting: "wf-err"}, true)

	if _, err := r.eng.TriggerWorkflow(context.Background(), "wf-main", nil); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	run := r.waitErrorRun(t, "wf-err")
	if st, _ := r.store.GetExecutionStatus(context.Background(), run.ID); st != "FAILED" {
		t.Fatalf("error workflow status = %s, want FAILED", st)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(r.errorRuns(t, "wf-err")); n != 1 {
		t.Fatalf("error workflow ran %d times, want 1", n)
	}
}

// Without a per-workflow setting the configured default is used.
func TestErrorWorkflowDefaultFromConfig(t *testing.T) {
	r := newErrorWorkflowRig(t, errors.New("boom"))
	db := r.store.RawDB()
	if _, err := db.Exec(`CREATE TABLE configs (name TEXT PRIMARY KEY, config_data TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO configs VALUES (?, ?)`, DefaultErrorWorkflowConfig, "wf-err"); err != nil {
		t.Fatal(err)
	}
	r.addWorkflow(t, "wf-err", TriggerNodeTypeError, "test.capture", nil, false)
	r.addWorkflow(t, "wf-main", "trigger.manual", "test.fail", nil, true)
	if _, err := r.eng.TriggerWorkflow(context.Background(), "wf-main", nil); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	r.waitErrorRun(t, "wf-err")
}

// A rejected run finishes REJECTED (class rejected) and starts no error workflow.
func TestRejectedRunIsNotFailed(t *testing.T) {
	r := newErrorWorkflowRig(t, errors.New("human_in_loop: "+ErrRejected.Error()))
	// The message alone must not make it REJECTED: only the typed error does.
	r.addWorkflow(t, "wf-err", TriggerNodeTypeError, "test.capture", nil, false)
	r.addWorkflow(t, "wf-main", "trigger.manual", "test.fail", map[string]interface{}{ErrorWorkflowSetting: "wf-err"}, true)
	execID, _ := r.eng.TriggerWorkflow(context.Background(), "wf-main", nil)
	if st := waitForTerminalStatus(t, r.store, execID, 10*time.Second); st != "FAILED" {
		t.Fatalf("untyped rejection text: status = %s, want FAILED", st)
	}

	r2 := newErrorWorkflowRig(t, wrapRejected())
	r2.addWorkflow(t, "wf-err", TriggerNodeTypeError, "test.capture", nil, false)
	r2.addWorkflow(t, "wf-main", "trigger.manual", "test.fail", map[string]interface{}{ErrorWorkflowSetting: "wf-err"}, true)
	execID, _ = r2.eng.TriggerWorkflow(context.Background(), "wf-main", nil)
	if st := waitForTerminalStatus(t, r2.store, execID, 10*time.Second); st != StatusRejected {
		t.Fatalf("status = %s, want %s", st, StatusRejected)
	}
	ex, _ := r2.store.GetExecution(context.Background(), execID)
	if ex.ErrorClass != string(ClassRejected) {
		t.Errorf("error_class = %q", ex.ErrorClass)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(r2.errorRuns(t, "wf-err")); n != 0 {
		t.Fatalf("a rejected run started %d error workflow runs", n)
	}
}

func wrapRejected() error { return errors.Join(errors.New("human_in_loop"), ErrRejected) }

// Settings survive the SQLite store round trip.
func TestWorkflowSettingsPersist(t *testing.T) {
	store := newFullEngineStore(t)
	ctx := context.Background()
	w := &Workflow{ProfileID: "default", ID: "w1", Name: "w", Settings: map[string]interface{}{ErrorWorkflowSetting: "x"}}
	if err := store.CreateWorkflow(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetWorkflow(ctx, "w1")
	if got.Settings[ErrorWorkflowSetting] != "x" {
		t.Fatalf("settings after create = %#v", got.Settings)
	}
	got.Settings = map[string]interface{}{ErrorWorkflowSetting: "y"}
	if err := store.UpdateWorkflow(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetWorkflow(ctx, "w1")
	if got.Settings[ErrorWorkflowSetting] != "y" {
		t.Fatalf("settings after update = %#v", got.Settings)
	}
	list, err := store.ListWorkflows(ctx, "default")
	if err != nil || len(list) != 1 || list[0].Settings[ErrorWorkflowSetting] != "y" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

// The file store keeps settings in the workflow's JSON.
func TestWorkflowSettingsPersistInFileStore(t *testing.T) {
	fs, err := NewWorkflowFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	w := &Workflow{ProfileID: "default", ID: "w1", Name: "w", Settings: map[string]interface{}{ErrorWorkflowSetting: "x"}}
	if err := fs.SaveWorkflow(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, err := fs.GetWorkflow(ctx, "w1")
	if err != nil || got == nil || got.Settings[ErrorWorkflowSetting] != "x" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
