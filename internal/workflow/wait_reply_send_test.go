package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeSender records the messages a send-and-wait node delivers.
type fakeSender struct {
	mu   sync.Mutex
	sent []string
	fail error
}

func (s *fakeSender) send(_ context.Context, r WaitReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.sent = append(s.sent, r.MessageText("Ship release 1.2?"))
	return nil
}

func (s *fakeSender) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sent...)
}

// sendAskNode is a test-only app node with n8n's sendAndWait operation:
// its config is sendAndWait's parameters, its channel the fake sender.
type sendAskNode struct{ s *fakeSender }

func (sendAskNode) Type() string { return "test.send_and_wait" }
func (n sendAskNode) Execute(ctx context.Context, in NodeInput, cfg map[string]interface{}) ([]NodeOutput, error) {
	spec, err := SendAndWaitSpec(cfg, time.Now())
	if err != nil {
		return nil, err
	}
	return SendAndWait(ctx, in, spec, n.s.send)
}

func TestSendAndWait_SendsOnceAndOutputs(t *testing.T) {
	f := newReplyFixture(t)
	s := &fakeSender{}
	spec := WaitReplySpec{Kind: WaitReplyApproval, ApprovalType: "double", ApproveLabel: "Ship"}
	for i := 0; i < 2; i++ {
		if _, err := SendAndWait(f.ctx, f.in, spec, s.send); !errors.Is(err, ErrNodePaused) {
			t.Fatalf("run %d: err = %v, want a pause", i, err)
		}
	}
	msgs := s.messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d messages, want 1 (a re-run before the answer sends nothing)", len(msgs))
	}
	if !strings.HasPrefix(msgs[0], "Ship release 1.2?\n\nDisapprove: https://hooks.example.com/reply/") || !strings.Contains(msgs[0], "\nShip: https://") {
		t.Fatalf("message = %q", msgs[0])
	}
	approve := msgs[0][strings.Index(msgs[0], "Ship: ")+len("Ship: "):]
	if rec := f.do(http.MethodPost, path(approve), nil); rec.Code != http.StatusOK {
		t.Fatalf("approve = %d", rec.Code)
	}
	out, err := SendAndWait(f.ctx, f.in, spec, s.send)
	if err != nil {
		t.Fatal(err)
	}
	if data := out[0].Items[0].JSON["data"].(map[string]interface{}); data["approved"] != true {
		t.Fatalf("output = %v", data)
	}
}

func TestSendAndWait_FailedSendDropsReply(t *testing.T) {
	f := newReplyFixture(t)
	s := &fakeSender{fail: errors.New("smtp down")}
	spec := WaitReplySpec{Kind: WaitReplyFreeText}
	if _, err := SendAndWait(f.ctx, f.in, spec, s.send); err == nil || err.Error() != "smtp down" {
		t.Fatalf("err = %v, want the send error", err)
	}
	s.fail = nil
	if _, err := SendAndWait(f.ctx, f.in, spec, s.send); !errors.Is(err, ErrNodePaused) {
		t.Fatalf("retry: err = %v", err)
	}
	if len(s.messages()) != 1 {
		t.Fatalf("retry sent %d messages", len(s.messages()))
	}
	var open int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM wait_replies WHERE consumed = 0`).Scan(&open)
	if open != 1 {
		t.Fatalf("%d open replies, want the retry's only", open)
	}
}

func TestSendAndWait_LimitPassesInputOn(t *testing.T) {
	f := newReplyFixture(t)
	s := &fakeSender{}
	in := f.in
	in.Items = []Item{{JSON: map[string]interface{}{"k": "v"}}}
	spec := WaitReplySpec{Kind: WaitReplyApproval, Limit: time.Now().Add(50 * time.Millisecond)}
	if _, err := SendAndWait(f.ctx, in, spec, s.send); !errors.Is(err, ErrNodePaused) {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	out, err := SendAndWait(f.ctx, in, spec, s.send)
	if err != nil || out[0].Items[0].JSON["k"] != "v" {
		t.Fatalf("out = %v, %v; want the input item", out, err)
	}
}

func TestSendAndWaitSpec(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	spec, err := SendAndWaitSpec(map[string]interface{}{
		"subject": "Deploy", "message": "Go?",
		"approvalOptions": map[string]interface{}{"values": map[string]interface{}{"approvalType": "double", "disapproveLabel": "No"}},
		"options":         map[string]interface{}{"limitWaitTime": map[string]interface{}{"values": map[string]interface{}{"resumeAmount": 45.0, "resumeUnit": "minutes"}}},
	}, now)
	if err != nil || spec.Kind != WaitReplyApproval || spec.Title != "Deploy" || spec.DisapproveLabel != "No" ||
		spec.ApprovalType != "double" || !spec.Limit.Equal(now.Add(45*time.Minute)) {
		t.Fatalf("approval spec = %+v, %v", spec, err)
	}
	spec, err = SendAndWaitSpec(map[string]interface{}{
		"responseType": "customForm", "defineForm": "json",
		"jsonOutput": `[{"fieldLabel":"Reason","fieldType":"textarea"}]`,
		"options":    map[string]interface{}{"messageButtonLabel": "Answer", "responseFormTitle": "Why?"},
	}, now)
	if err != nil || spec.FormFields[0].Label != "Reason" || spec.LinkLabel != "Answer" || spec.Title != "Why?" {
		t.Fatalf("form spec = %+v, %v", spec, err)
	}
	if _, err := SendAndWaitSpec(map[string]interface{}{"responseType": "vote"}, now); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("bad responseType: %v", err)
	}
}

func TestWaitReplyMessageHTML(t *testing.T) {
	r := replyOf(strings.Repeat("a", 43), WaitReplyApproval, WaitReplySpec{ApprovalType: "double", ApproveLabel: "<b>Yes</b>"})
	h := r.MessageHTML("line1\nline <2>")
	for _, want := range []string{"line1<br>line &lt;2&gt;", "&lt;b&gt;Yes&lt;/b&gt;", `href="` + r.ApproveURL + `"`, ">Disapprove</a>"} {
		if !strings.Contains(h, want) {
			t.Errorf("html lacks %q: %s", want, h)
		}
	}
}

// End to end through an engine: the node's message goes out once, the
// approval wakes the run, and the node outputs {data: {approved: true}}.
func TestSendAndWait_ThroughEngine(t *testing.T) {
	fixedReplyKey(t)
	store, _ := newMigratedStore(t)
	s := &fakeSender{}
	reg := NewNodeTypeRegistry()
	reg.Register("test.send_and_wait", func() NodeExecutor { return sendAskNode{s: s} })
	eng := NewWorkflowEngineWithStore(store, store.RawDB(), nil, reg, EngineConfig{ProfileID: "default"}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); _ = eng.Stop() })
	eng.queue.Start(ctx)
	wf := &Workflow{ID: "wf-sw", Name: "wf-sw", ProfileID: "default",
		Nodes: []WorkflowNode{
			{ID: "t", WorkflowID: "wf-sw", Type: "trigger.manual", Name: "T"},
			{ID: "s", WorkflowID: "wf-sw", Type: "test.send_and_wait", Name: "Ask", Config: map[string]interface{}{"message": "Ship?"}},
		},
		Connections: []WorkflowConnection{{SourceNodeID: "t", SourceHandle: "main", TargetNodeID: "s", TargetHandle: "main"}},
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
	for deadline := time.Now().Add(10 * time.Second); len(s.messages()) == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("no message was sent")
		}
	}
	msg := s.messages()[0]
	approve := msg[strings.Index(msg, "Approve: ")+len("Approve: "):]
	rec := httptest.NewRecorder()
	eng.WebhookHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path(approve), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", rec.Code, rec.Body.String())
	}
	if st := waitForTerminalStatus(t, store, execID, 10*time.Second); st != "SUCCESS" {
		t.Fatalf("run = %s", st)
	}
	if n := len(s.messages()); n != 1 {
		t.Fatalf("sent %d messages", n)
	}
	exec, _ := store.GetExecution(ctx, execID)
	for _, n := range exec.Nodes {
		if n.NodeID == "s" && n.Status == "SUCCESS" {
			if data, _ := n.OutputItems[0].JSON["data"].(map[string]interface{}); data["approved"] != true {
				t.Fatalf("output = %v", n.OutputItems)
			}
			return
		}
	}
	t.Fatalf("no successful send-and-wait node in %+v", exec.Nodes)
}

// A dispatch-and-wait callback's body becomes the items, as n8n's resume
// webhook outputs it.
func TestCallBodyItems(t *testing.T) {
	list := callBodyItems([]interface{}{map[string]interface{}{"a": 1.0}, "x"})
	if len(list) != 2 || list[0].JSON["a"] != 1.0 || list[1].JSON["data"] != "x" {
		t.Fatalf("list = %+v", list)
	}
	if one := callBodyItems(nil); len(one) != 1 || len(one[0].JSON) != 0 {
		t.Fatalf("empty body = %+v", one)
	}
}
