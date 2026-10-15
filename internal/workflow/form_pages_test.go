package workflow

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A page answered while the run is slow to show the next step redirects to
// the resume URL, which waits (refreshing) and then shows the ending.
func TestFormPages_WaitPageThenEnding(t *testing.T) {
	f := newReplyFixture(t)
	old := formStepWait
	formStepWait = 50 * time.Millisecond
	t.Cleanup(func() { formStepWait = old })

	r, err := NewWaitReply(f.ctx, f.in, WaitReplySpec{Kind: WaitReplyFormPage, ExecutionURL: true, Title: "Step",
		FormFields: []WaitReplyField{{Label: "Q", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := ExecutionResumeURL("e1"); r.URL != want {
		t.Fatalf("page URL = %s, want the resume URL %s", r.URL, want)
	}
	rec := f.do(http.MethodPost, path(r.URL), url.Values{"f0": {"yes"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != path(r.URL) {
		t.Fatalf("answer = %d %v", rec.Code, rec.Header())
	}
	res, _ := WaitReplyAnswer(f.ctx, f.in)
	if res == nil || res.Answer["Q"] != "yes" || res.Answer["formMode"] != "production" || res.Answer["submittedAt"] == nil {
		t.Fatalf("answer = %+v", res)
	}
	rec = f.do(http.MethodGet, path(r.URL), nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Refresh") == "" || !strings.Contains(rec.Body.String(), "being prepared") {
		t.Fatalf("wait page = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}

	if err := EndFormFlow(f.ctx, NodeInput{WorkflowID: "wf", ExecutionID: "e1", NodeID: "end"},
		WaitReplySpec{RespondWith: "showText", ResponseText: "<h1>Bye</h1><script>x()</script>"}); err != nil {
		t.Fatal(err)
	}
	rec = f.do(http.MethodGet, path(r.URL), nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "<h1>Bye</h1><script>x()</script>" ||
		!strings.HasPrefix(rec.Header().Get("Content-Security-Policy"), "sandbox;") {
		t.Fatalf("ending = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	// The ending is a step, not a reply: nothing waits on it.
	if res, _ := WaitReplyAnswer(f.ctx, NodeInput{ExecutionID: "e1", NodeID: "end"}); res != nil {
		t.Fatalf("ending left an open reply: %+v", res)
	}
}

func TestEndFormFlowChecksConfig(t *testing.T) {
	f := newReplyFixture(t)
	for _, spec := range []WaitReplySpec{{RespondWith: "redirect", RedirectURL: "javascript:alert(1)"}, {RespondWith: "returnBinary"}} {
		if err := EndFormFlow(f.ctx, f.in, spec); err == nil {
			t.Errorf("%+v was accepted", spec)
		}
	}
}

func TestWorkflowHasFormSteps(t *testing.T) {
	plain := &Workflow{Nodes: []WorkflowNode{{Type: "trigger.form"}, {Type: "core.wait", Config: map[string]interface{}{"resume": "webhook"}}}}
	if workflowHasFormSteps(plain) {
		t.Error("a workflow without pages holds submissions")
	}
	for _, n := range []WorkflowNode{{Type: "core.form"}, {Type: "core.wait", Config: map[string]interface{}{"resume": "form"}}} {
		if !workflowHasFormSteps(&Workflow{Nodes: []WorkflowNode{{Type: "trigger.form"}, n}}) {
			t.Errorf("%s %v is not a form step", n.Type, n.Config)
		}
	}
}
