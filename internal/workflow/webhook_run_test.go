package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/account"
)

// fakeExecs is a WebhookExecutions whose run status the test sets.
type fakeExecs struct {
	mu     sync.Mutex
	status string
	exec   *WorkflowExecution
}

func (f *fakeExecs) set(st string) { f.mu.Lock(); f.status = st; f.mu.Unlock() }

func (f *fakeExecs) GetExecutionStatus(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *fakeExecs) GetExecution(context.Context, string) (*WorkflowExecution, error) {
	return f.exec, nil
}

func (f *fakeExecs) ExecutionInFlight(ctx context.Context, id string) (string, bool, error) {
	st, err := f.GetExecutionStatus(ctx, id)
	return st, st == "QUEUED" || st == "RUNNING", err
}

// runHook serves one webhook through a runner. run may be nil for one that
// starts "exec-1" and records the items.
func runHook(t *testing.T, reg *WebhookRegistration, run WebhookRunner, execs WebhookExecutions) *WebhookServer {
	t.Helper()
	old := webhookPollInterval
	webhookPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { webhookPollInterval = old })
	s := NewWebhookServer(":0", zerolog.Nop())
	reg.Path, reg.Method, reg.WorkflowID, reg.NodeID = "hook", "POST", "wf-1", "t1"
	reg.TriggerFn = func([]Item) { t.Error("TriggerFn called although the server has a runner") }
	if err := s.Register(reg); err != nil {
		t.Fatal(err)
	}
	if run == nil {
		run = func(string, string, []Item, func(string)) (string, error) { return "exec-1", nil }
	}
	if execs == nil {
		execs = &fakeExecs{}
	}
	s.SetRunner(run, execs)
	return s
}

func postHook(s *WebhookServer, body string, header ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhook/hook", strings.NewReader(body))
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response %q is not a JSON object: %v", rec.Body.String(), err)
	}
	return m
}

func TestWebhookRun_OnReceivedReturnsExecutionID(t *testing.T) {
	s := runHook(t, &WebhookRegistration{}, nil, nil)
	rec := postHook(s, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if m := decode(t, rec); m["success"] != true || m["execution_id"] != "exec-1" {
		t.Errorf("response = %v, want success and execution_id", m)
	}
}

func TestWebhookRun_OnReceivedResponseCode(t *testing.T) {
	s := runHook(t, &WebhookRegistration{ResponseCode: http.StatusAccepted}, nil, nil)
	if rec := postHook(s, `{}`); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
}

// A delivery that starts no run is not answered 200.
func TestWebhookRun_DroppedRunsAreNot200(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{ErrWebhookQueueFull, http.StatusServiceUnavailable},
		{&account.LoginRequiredError{}, http.StatusServiceUnavailable},
		{ErrWebhookWorkflowNotFound, http.StatusNotFound},
		{ErrWebhookWorkflowInactive, http.StatusConflict},
		{errors.New("disk on fire"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		run := func(string, string, []Item, func(string)) (string, error) { return "", tc.err }
		s := runHook(t, &WebhookRegistration{}, run, nil)
		rec := postHook(s, `{}`)
		if rec.Code != tc.want {
			t.Errorf("%v: status = %d, want %d", tc.err, rec.Code, tc.want)
		}
		if m := decode(t, rec); m["error"] == nil || m["success"] == true {
			t.Errorf("%v: response = %v, want a JSON error", tc.err, m)
		}
	}
}

func TestWebhookRun_Dedupe(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	run := func(string, string, []Item, func(string)) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		runs++
		return fmt.Sprintf("exec-%d", runs), nil
	}
	s := runHook(t, &WebhookRegistration{DedupeHeader: "X-GitHub-Delivery"}, run, nil)
	s.SetDeliveryStore(NewMemoryWebhookDeliveries())

	first := decode(t, postHook(s, `{}`, "X-GitHub-Delivery", "d-1"))
	dup := postHook(s, `{}`, "X-GitHub-Delivery", "d-1")
	if dup.Code != http.StatusOK {
		t.Fatalf("duplicate: status = %d, want 200", dup.Code)
	}
	if m := decode(t, dup); m["duplicate"] != true || m["execution_id"] != first["execution_id"] {
		t.Errorf("duplicate response = %v, want duplicate with %v", m, first["execution_id"])
	}
	postHook(s, `{}`, "X-GitHub-Delivery", "d-2")
	postHook(s, `{}`) // no delivery id: never a duplicate
	if runs != 3 {
		t.Errorf("runs = %d, want 3 (d-1, d-2, no id)", runs)
	}
}

// A delivery that started no run is forgotten, so the sender's retry runs.
func TestWebhookRun_DedupeReleasedWhenNoRun(t *testing.T) {
	fail := true
	run := func(string, string, []Item, func(string)) (string, error) {
		if fail {
			return "", ErrWebhookQueueFull
		}
		return "exec-ok", nil
	}
	s := runHook(t, &WebhookRegistration{DedupeHeader: "Idempotency-Key"}, run, nil)
	s.SetDeliveryStore(NewMemoryWebhookDeliveries())
	if rec := postHook(s, `{}`, "Idempotency-Key", "k"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("first: status = %d, want 503", rec.Code)
	}
	fail = false
	m := decode(t, postHook(s, `{}`, "Idempotency-Key", "k"))
	if m["duplicate"] == true || m["execution_id"] != "exec-ok" {
		t.Errorf("retry = %v, want a new run", m)
	}
}

func TestSQLWebhookDeliveries(t *testing.T) {
	_, db := newMigratedStore(t)
	d := NewSQLWebhookDeliveries(db)
	ctx := context.Background()

	if ok, _, err := d.Claim(ctx, "wf", "a"); err != nil || !ok {
		t.Fatalf("first claim = %v, %v; want claimed", ok, err)
	}
	if err := d.Attach(ctx, "wf", "a", "exec-a"); err != nil {
		t.Fatal(err)
	}
	if ok, exec, err := d.Claim(ctx, "wf", "a"); err != nil || ok || exec != "exec-a" {
		t.Fatalf("second claim = %v, %q, %v; want duplicate of exec-a", ok, exec, err)
	}
	if ok, _, _ := d.Claim(ctx, "wf-other", "a"); !ok {
		t.Error("the same id for another workflow must not be a duplicate")
	}
	if err := d.Release(ctx, "wf", "a"); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := d.Claim(ctx, "wf", "a"); !ok {
		t.Error("a released id must be claimable again")
	}

	// Rows past the TTL are pruned on the next due claim.
	if _, err := db.Exec(`UPDATE webhook_deliveries SET created_at = datetime('now', '-8 days') WHERE delivery_id = 'a'`); err != nil {
		t.Fatal(err)
	}
	d.lastPrune = time.Time{}
	if ok, _, _ := d.Claim(ctx, "wf", "a"); !ok {
		t.Error("an id older than 7 days must be forgotten")
	}
}

func TestWebhookRun_LastNodeFirstEntry(t *testing.T) {
	execs := &fakeExecs{status: "RUNNING", exec: &WorkflowExecution{Nodes: []WorkflowExecutionNode{
		{NodeID: "t1", Status: "SUCCESS", InputItems: []Item{{JSON: map[string]interface{}{"in": true}}}},
		{NodeID: "n1", Status: "SUCCESS", OutputItems: []Item{{JSON: map[string]interface{}{"answer": 42}}, {JSON: map[string]interface{}{"answer": 43}}}},
	}}}
	s := runHook(t, &WebhookRegistration{ResponseMode: WebhookRespondLastNode}, nil, execs)
	time.AfterFunc(20*time.Millisecond, func() { execs.set("SUCCESS") })
	rec := postHook(s, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if m := decode(t, rec); m["answer"] != float64(42) {
		t.Errorf("response = %v, want the last node's first item", m)
	}
}

func TestWebhookRun_LastNodeAllEntries(t *testing.T) {
	execs := &fakeExecs{status: "SUCCESS", exec: &WorkflowExecution{Nodes: []WorkflowExecutionNode{
		{NodeID: "n1", Status: "SUCCESS", OutputItems: []Item{{JSON: map[string]interface{}{"a": 1}}, {JSON: map[string]interface{}{"a": 2}}}},
	}}}
	s := runHook(t, &WebhookRegistration{ResponseMode: WebhookRespondLastNode, ResponseData: "allEntries"}, nil, execs)
	rec := postHook(s, `{}`)
	var list []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 2 {
		t.Fatalf("response %q, want a list of 2", rec.Body.String())
	}
}

func TestWebhookRun_LastNodeFailedRun(t *testing.T) {
	execs := &fakeExecs{status: "FAILED"}
	s := runHook(t, &WebhookRegistration{ResponseMode: WebhookRespondLastNode}, nil, execs)
	rec := postHook(s, `{}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if m := decode(t, rec); m["execution_id"] != "exec-1" {
		t.Errorf("response = %v, want the execution_id", m)
	}
}

func TestWebhookRun_WaitTimesOutWith202(t *testing.T) {
	execs := &fakeExecs{status: "RUNNING"}
	s := runHook(t, &WebhookRegistration{ResponseMode: WebhookRespondLastNode, ResponseTimeout: 30 * time.Millisecond}, nil, execs)
	rec := postHook(s, `{}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if m := decode(t, rec); m["execution_id"] != "exec-1" {
		t.Errorf("response = %v, want the execution_id", m)
	}
}

func TestWebhookRun_ResponseNode(t *testing.T) {
	execs := &fakeExecs{status: "RUNNING"}
	run := func(_, _ string, _ []Item, prepare func(string)) (string, error) {
		prepare("exec-rn")
		go DeliverWebhookResponse("exec-rn", WebhookResponse{
			StatusCode: http.StatusCreated,
			Headers:    map[string]string{"Content-Type": "text/plain", "X-Bad\r\nInjected": "x"},
			Body:       []byte("made it"),
		})
		return "exec-rn", nil
	}
	s := runHook(t, &WebhookRegistration{ResponseMode: WebhookRespondNode}, run, execs)
	rec := postHook(s, `{}`)
	if rec.Code != http.StatusCreated || rec.Body.String() != "made it" {
		t.Fatalf("got %d %q, want 201 made it", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	if DeliverWebhookResponse("exec-rn", WebhookResponse{}) {
		t.Error("the responder must be gone once the request is answered")
	}
}

// A run that ends without reaching a Respond node is answered by its status.
func TestWebhookRun_ResponseNodeNeverRan(t *testing.T) {
	execs := &fakeExecs{status: "SUCCESS"}
	s := runHook(t, &WebhookRegistration{ResponseMode: WebhookRespondNode}, nil, execs)
	rec := postHook(s, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if m := decode(t, rec); m["execution_id"] != "exec-1" {
		t.Errorf("response = %v", m)
	}
}

func TestDeliverWebhookResponse_NobodyWaiting(t *testing.T) {
	if DeliverWebhookResponse("no-such-exec", WebhookResponse{}) {
		t.Error("delivery with no waiting request reported true")
	}
}

func TestApplyWebhookOptions(t *testing.T) {
	reg := &WebhookRegistration{}
	err := applyWebhookOptions(reg, map[string]interface{}{
		"response_mode": "lastNode", "response_data": "allEntries", "response_code": "201",
		"response_timeout": float64(5), "max_body_bytes": float64(1 << 30), "dedupe_header": " X-GitHub-Delivery ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reg.ResponseMode != "lastNode" || reg.ResponseData != "allEntries" || reg.ResponseCode != 201 ||
		reg.ResponseTimeout != 5*time.Second || reg.MaxBodyBytes != maxWebhookMaxBody || reg.DedupeHeader != "X-GitHub-Delivery" {
		t.Errorf("reg = %+v", reg)
	}
	for _, bad := range []map[string]interface{}{
		{"response_mode": "whenever"},
		{"response_data": "some"},
		{"response_code": float64(500)},
		{"response_timeout": "soon"},
	} {
		if err := applyWebhookOptions(&WebhookRegistration{}, bad); err == nil {
			t.Errorf("%v: want an error", bad)
		}
	}
}
