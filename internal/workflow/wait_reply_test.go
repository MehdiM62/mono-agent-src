package workflow

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/vault"
)

// fixedReplyKey signs reply tokens with a test key instead of the
// machine's trace key file.
func fixedReplyKey(t *testing.T) {
	t.Helper()
	old := replyKey
	replyKey = func() ([]byte, error) { return []byte("0123456789abcdef0123456789abcdef"), nil }
	t.Cleanup(func() { replyKey = old })
}

// replyFixture is a migrated database with one WAITING execution and a
// reply handler over it.
type replyFixture struct {
	db    *sql.DB
	ctx   context.Context
	in    NodeInput
	h     *replyHandler
	woken chan string
}

func newReplyFixture(t *testing.T) *replyFixture {
	t.Helper()
	fixedReplyKey(t)
	t.Setenv("MONOAGENT_PUBLIC_URL", "https://hooks.example.com/")
	store, db := newMigratedStore(t)
	if err := store.CreateWorkflow(context.Background(), &Workflow{ID: "wf", Name: "wf", ProfileID: "default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO workflow_executions (id, workflow_id, status) VALUES ('e1', 'wf', 'WAITING')`); err != nil {
		t.Fatal(err)
	}
	f := &replyFixture{db: db, ctx: vault.ContextWithDB(context.Background(), db),
		in: NodeInput{WorkflowID: "wf", ExecutionID: "e1", NodeID: "n1"}, woken: make(chan string, 4)}
	f.h = &replyHandler{db: db, logger: zerolog.Nop(), wake: func(id string) { f.woken <- id }}
	return f
}

func (f *replyFixture) do(method, target string, form url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func path(u string) string {
	p, _ := url.Parse(u)
	return p.Path
}

func TestWaitReply_ApprovalLinks(t *testing.T) {
	f := newReplyFixture(t)
	r, err := NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyApproval, ApprovalType: "double", Title: "Ship it?"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.ApproveURL, "https://hooks.example.com/reply/") || !strings.HasSuffix(r.ApproveURL, "/approve") ||
		r.DisapproveURL != r.URL+"/decline" || r.FormURL != "" {
		t.Fatalf("links = %+v", r)
	}
	again, err := NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyApproval})
	if err != nil || again.Token != r.Token {
		t.Fatalf("second NewWaitReply = %q, %v; want the open reply %q", again.Token, err, r.Token)
	}
	if res, _ := WaitReplyAnswer(f.ctx, f.in); res == nil || res.Status != WaitReplyPending || res.Sent {
		t.Fatalf("before an answer: %+v", res)
	}

	// Opening a link only shows a page: a link scanner answers nothing.
	if rec := f.do(http.MethodGet, path(r.DisapproveURL), nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Decline") {
		t.Fatalf("GET decline = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodGet, path(r.URL), nil); !strings.Contains(rec.Body.String(), r.Token+"/approve") {
		t.Fatalf("reply page lacks the approve button: %s", rec.Body.String())
	}
	if res, _ := WaitReplyAnswer(f.ctx, f.in); res.Status != WaitReplyPending {
		t.Fatalf("a GET recorded an answer: %+v", res)
	}

	if rec := f.do(http.MethodPost, path(r.DisapproveURL), nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "declined") {
		t.Fatalf("POST decline = %d %s", rec.Code, rec.Body.String())
	}
	if id := <-f.woken; id != "e1" {
		t.Fatalf("woke %q", id)
	}
	// A second click (either button) shows the recorded answer.
	if rec := f.do(http.MethodPost, path(r.ApproveURL), nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "declined") {
		t.Fatalf("second click = %d %s", rec.Code, rec.Body.String())
	}

	res, err := WaitReplyAnswer(f.ctx, f.in)
	if err != nil || res.Status != WaitReplyAnswered {
		t.Fatalf("answer = %+v, %v", res, err)
	}
	data := res.Items()[0].JSON["data"].(map[string]interface{})
	if data["approved"] != false || data["respondedAt"] == "" {
		t.Fatalf("output = %v", data)
	}
	// Read once: the next run (a loop's next iteration) asks anew.
	if res, _ := WaitReplyAnswer(f.ctx, f.in); res != nil {
		t.Fatalf("answer read twice: %+v", res)
	}
	next, err := NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyApproval})
	if err != nil || next.Token == r.Token || next.DisapproveURL != "" {
		t.Fatalf("next run's reply = %+v, %v", next, err)
	}
	if rec := f.do(http.MethodPost, path(next.URL)+"/decline", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("decline on a single approval = %d", rec.Code)
	}
}

func TestWaitReply_RefusesUnknownExpiredAndFinished(t *testing.T) {
	f := newReplyFixture(t)
	if rec := f.do(http.MethodGet, "/reply/"+strings.Repeat("a", 43), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown token = %d", rec.Code)
	}
	if rec := f.do(http.MethodGet, "/reply/../x", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("malformed token = %d", rec.Code)
	}

	r, _ := NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyApproval, Limit: time.Now().Add(-time.Second)})
	if rec := f.do(http.MethodPost, path(r.ApproveURL), nil); rec.Code != http.StatusGone {
		t.Fatalf("expired = %d", rec.Code)
	}
	res, _ := WaitReplyAnswer(f.ctx, f.in)
	if res == nil || res.Status != WaitReplyExpired {
		t.Fatalf("expired reply = %+v", res)
	}

	r, _ = NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyApproval})
	_, _ = f.db.Exec(`UPDATE workflow_executions SET status='CANCELLED'`)
	if rec := f.do(http.MethodPost, path(r.ApproveURL), nil); rec.Code != http.StatusGone {
		t.Fatalf("finished run = %d", rec.Code)
	}
}

func TestWaitReply_CustomForm(t *testing.T) {
	f := newReplyFixture(t)
	spec := WaitReplySpec{Kind: WaitReplyCustomForm, FormFields: []WaitReplyField{
		{Label: "Name", Required: true},
		{Label: "Age", Type: "number"},
		{Label: "Tags", Type: "checkbox", Options: []string{"a", "b"}},
		{Label: "Pick", Name: "pick", Type: "dropdown", Options: []string{"x", "y"}},
	}}
	r, err := NewWaitReply(f.ctx, f.in, spec)
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.do(http.MethodGet, path(r.FormURL), nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `name="f3"`) {
		t.Fatalf("form = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodPost, path(r.FormURL), url.Values{"f1": {"3"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing required = %d", rec.Code)
	}
	if rec := f.do(http.MethodPost, path(r.FormURL), url.Values{"f0": {"Ada"}, "f3": {"z"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown option = %d", rec.Code)
	}
	if rec := f.do(http.MethodPost, path(r.FormURL), url.Values{"f0": {"Ada"}, "f1": {"36"}, "f2": {"a", "b"}, "f3": {"y"}}); rec.Code != http.StatusOK {
		t.Fatalf("submit = %d %s", rec.Code, rec.Body.String())
	}
	res, _ := WaitReplyAnswer(f.ctx, f.in)
	a := res.Answer
	if a["Name"] != "Ada" || a["Age"] != 36.0 || len(a["Tags"].([]interface{})) != 2 || a["pick"] != "y" {
		t.Fatalf("answer = %v", a)
	}

	if _, err := NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyCustomForm}); err == nil {
		t.Fatal("a form without fields was accepted")
	}
}

func TestWaitReply_FreeText(t *testing.T) {
	f := newReplyFixture(t)
	r, _ := NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyFreeText})
	if rec := f.do(http.MethodPost, path(r.FormURL), url.Values{"f0": {"looks good"}}); rec.Code != http.StatusOK {
		t.Fatalf("submit = %d", rec.Code)
	}
	res, _ := WaitReplyAnswer(f.ctx, f.in)
	if got := res.Items()[0].JSON["data"].(map[string]interface{})["text"]; got != "looks good" {
		t.Fatalf("text = %v", got)
	}
}

func TestWaitReply_WebhookOnExecutionURL(t *testing.T) {
	f := newReplyFixture(t)
	r, err := NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyWebhook, ExecutionURL: true, HTTPMethod: "POST"})
	if err != nil {
		t.Fatal(err)
	}
	resume, _ := ExecutionResumeURL("e1")
	if r.URL != resume {
		t.Fatalf("url = %s, want the execution's resume URL %s", r.URL, resume)
	}
	if rec := f.do(http.MethodGet, path(resume)+"?a=1", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET on a POST resume = %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, path(resume)+"?a=1", strings.NewReader(`{"ok":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume = %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path(resume), strings.NewReader(`{}`)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second call = %d", rec.Code)
	}
	res, _ := WaitReplyAnswer(f.ctx, f.in)
	item := res.Items()[0].JSON
	if item["body"].(map[string]interface{})["ok"] != true || item["query"].(map[string]interface{})["a"] != "1" {
		t.Fatalf("item = %v", item)
	}
}

func TestPublicBaseURL(t *testing.T) {
	t.Setenv("MONOAGENT_PUBLIC_URL", "")
	t.Setenv("MONOAGENT_WEBHOOK_ADDR", "")
	if got := PublicBaseURL(); got != "http://127.0.0.1:9321" {
		t.Errorf("default = %s", got)
	}
	t.Setenv("MONOAGENT_WEBHOOK_ADDR", "10.0.0.5:8080")
	if got := PublicBaseURL(); got != "https://10.0.0.5:8080" {
		t.Errorf("LAN bind = %s", got)
	}
	t.Setenv("MONOAGENT_PUBLIC_URL", "https://x.example/hooks/")
	if got := PublicBaseURL(); got != "https://x.example/hooks" {
		t.Errorf("public = %s", got)
	}
}

// askNode waits for an approval through the reply primitive.
type askNode struct {
	mu    *sync.Mutex
	links *[]WaitReply
}

func (askNode) Type() string { return "test.ask" }
func (n askNode) Execute(ctx context.Context, in NodeInput, _ map[string]interface{}) ([]NodeOutput, error) {
	res, err := WaitReplyAnswer(ctx, in)
	if err != nil {
		return nil, err
	}
	if res == nil {
		r, err := NewWaitReply(ctx, in, WaitReplySpec{Kind: WaitReplyApproval})
		if err != nil {
			return nil, err
		}
		n.mu.Lock()
		*n.links = append(*n.links, r)
		n.mu.Unlock()
		return nil, r.Pause()
	}
	if res.Status == WaitReplyPending {
		return nil, res.Pause()
	}
	return []NodeOutput{{Handle: "main", Items: res.Items()}}, nil
}

// An answer wakes the paused run at once: no resume loop runs here, so
// only the reply's wake can finish it.
func TestWaitReply_AnswerWakesRun(t *testing.T) {
	fixedReplyKey(t)
	store, _ := newMigratedStore(t)
	var mu sync.Mutex
	var links []WaitReply
	reg := NewNodeTypeRegistry()
	reg.Register("test.ask", func() NodeExecutor { return askNode{mu: &mu, links: &links} })
	eng := NewWorkflowEngineWithStore(store, store.RawDB(), nil, reg, EngineConfig{ProfileID: "default"}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = eng.Stop() })
	eng.queue.Start(ctx)
	wf := &Workflow{ID: "wf-ask", Name: "wf-ask", ProfileID: "default",
		Nodes: []WorkflowNode{
			{ID: "t", WorkflowID: "wf-ask", Type: "trigger.manual", Name: "T"},
			{ID: "a", WorkflowID: "wf-ask", Type: "test.ask", Name: "Ask"},
		},
		Connections: []WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "a", TargetHandle: "main"}},
	}
	if err := eng.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkflowActive(ctx, wf.ID, true); err != nil {
		t.Fatal(err)
	}
	execID, err := eng.TriggerWorkflow(ctx, wf.ID, map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for st, _ := store.GetExecutionStatus(ctx, execID); st != "WAITING"; st, _ = store.GetExecutionStatus(ctx, execID) {
		if time.Now().After(deadline) {
			t.Fatalf("run = %s, want WAITING", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	link := links[0].ApproveURL
	mu.Unlock()
	rec := httptest.NewRecorder()
	eng.webhookServer.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path(link), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body.String())
	}
	if st := waitForTerminalStatus(t, store, execID, 10*time.Second); st != "SUCCESS" {
		t.Fatalf("run = %s, want SUCCESS", st)
	}
	exec, _ := store.GetExecution(ctx, execID)
	for _, n := range exec.Nodes {
		if n.NodeID == "a" && n.Status == "SUCCESS" {
			if data, _ := n.OutputItems[0].JSON["data"].(map[string]interface{}); data["approved"] != true {
				t.Fatalf("output = %v", n.OutputItems)
			}
			return
		}
	}
	t.Fatalf("no successful Ask node in %+v", exec.Nodes)
}
