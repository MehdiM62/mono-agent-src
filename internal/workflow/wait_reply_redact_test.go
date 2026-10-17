package workflow

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/commsync"
)

// A reply's token is hidden wherever it appears in what an agent is shown:
// in a link (URL-encoded too) and bare, for a reply that lets a message go
// out, one opened before that was recorded, a link to no recorded reply,
// and the shown execution's resume URL before its wait opens. A plain
// reply's token, and a 43-character string that is no reply's, stay.
func TestReplyRedactorHidesTheTokensOfMarkedReplies(t *testing.T) {
	sendsByType(t)
	f := newReplyFixture(t)
	mail := commsync.Reachable{Nodes: []commsync.Node{{Type: "comm.email_send"}}}
	marked, err := NewWaitReply(after(f.ctx, mail), f.in, WaitReplySpec{Kind: WaitReplyApproval})
	if err != nil {
		t.Fatal(err)
	}
	quietIn := f.in
	quietIn.NodeID = "n2"
	quiet, err := NewWaitReply(after(f.ctx, commsync.Reachable{}), quietIn, WaitReplySpec{Kind: WaitReplyWebhook})
	if err != nil {
		t.Fatal(err)
	}
	old := strings.Repeat("o", replyTokenLen)
	if _, err := f.db.Exec(`INSERT INTO wait_replies (execution_id, node_id, run_index, workflow_id, token, kind, spec)
		VALUES ('e0', 'n', 0, 'wf', ?, 'webhook', '{"kind":"webhook"}')`, old); err != nil {
		t.Fatal(err)
	}
	resume, err := ExecutionResumeURL("e1")
	if err != nil {
		t.Fatal(err)
	}
	resumeTok := resume[strings.LastIndex(resume, "/")+1:]
	unknown := strings.Repeat("u", replyTokenLen)
	other := strings.Repeat("x", replyTokenLen)

	in := strings.Join([]string{
		"approve: " + marked.ApproveURL, "bare " + marked.Token, "encoded https%3A%2F%2Fh%2Freply%2F" + marked.Token,
		"old " + old, "resume " + resume, "id=" + resumeTok + "&x", "nowhere https://h/reply/" + unknown,
		"quiet " + quiet.URL, "bare quiet " + quiet.Token, "other " + other,
	}, "\n")
	got := NewReplyRedactor(f.ctx, f.db, "e1").String(in)
	for _, tok := range []string{marked.Token, old, resumeTok, unknown} {
		if strings.Contains(got, tok) {
			t.Errorf("token %s stayed:\n%s", tok, got)
		}
	}
	for _, keep := range []string{quiet.URL, "bare quiet " + quiet.Token, "other " + other, "approve: https://hooks.example.com/reply/" + ReplyTokenHidden + "/approve"} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost %q:\n%s", keep, got)
		}
	}
	if strings.Count(got, ReplyTokenHidden) != 7 {
		t.Errorf("want 7 hidden tokens:\n%s", got)
	}

	if (*ReplyRedactor)(nil).String(in) != in {
		t.Error("a nil redactor changed the text")
	}
	// No database to tell: every link's token and every bare 43-character run.
	blind := NewReplyRedactor(f.ctx, nil).String(in)
	if strings.Contains(blind, quiet.Token) || strings.Contains(blind, other) {
		t.Errorf("without a database a candidate stayed:\n%s", blind)
	}
}

// Items, binary data and an execution's records are covered, and the
// values handed in are left as they were.
func TestReplyRedactorCoversItemsAndExecutions(t *testing.T) {
	f := newReplyFixture(t)
	resume, err := ExecutionResumeURL("e1")
	if err != nil {
		t.Fatal(err)
	}
	item := Item{JSON: map[string]interface{}{"resume": resume, "nested": []interface{}{map[string]interface{}{resume: 1.0}}, "n": 2.0},
		Binary: map[string][]byte{"page": []byte("<a href=" + resume + ">")}}
	rr := NewReplyRedactor(f.ctx, f.db, "e1")
	got := rr.Items([]Item{item})
	if s := got[0].JSON["resume"].(string); strings.Contains(s, "/reply/") && !strings.HasSuffix(s, ReplyTokenHidden) {
		t.Errorf("resume = %q", s)
	}
	if _, ok := got[0].JSON["nested"].([]interface{})[0].(map[string]interface{})[resume]; ok || got[0].JSON["n"] != 2.0 {
		t.Errorf("json = %v", got[0].JSON)
	}
	if strings.Contains(string(got[0].Binary["page"]), resume) || item.JSON["resume"] != resume {
		t.Errorf("binary %q, input changed: %v", got[0].Binary["page"], item.JSON["resume"] != resume)
	}

	e := &WorkflowExecution{ID: "e1", ErrorMessage: "posting " + resume + " failed", TriggerData: map[string]interface{}{"cb": resume},
		Nodes: []WorkflowExecutionNode{{InputItems: []Item{item}, OutputItems: []Item{item}, ErrorMessage: resume, ErrorHint: resume}}}
	rr.Execution(e)
	n := e.Nodes[0]
	for _, s := range []string{e.ErrorMessage, e.TriggerData["cb"].(string), n.InputItems[0].JSON["resume"].(string),
		n.OutputItems[0].JSON["resume"].(string), n.ErrorMessage, n.ErrorHint} {
		if strings.Contains(s, resume) {
			t.Errorf("execution kept the resume URL: %q", s)
		}
	}
}

// An approval's edited data holding the placeholder would write it over the
// reply link the run needs: refused.
func TestCheckReplyEdit(t *testing.T) {
	if err := CheckReplyEdit(`{"caption":"edited"}`); err != nil {
		t.Errorf("plain edit refused: %v", err)
	}
	if err := CheckReplyEdit(`{"link":"https://h/reply/` + ReplyTokenHidden + `"}`); err == nil || !strings.Contains(err.Error(), "leave out the fields") {
		t.Errorf("err = %v", err)
	}
}

// An execution's resume URL and a chat session's token answer other replies
// too, so they stay hidden even when every reply open under them sends
// nothing, in the text and the waiting list; a node's own reply that sends
// nothing keeps its token and its link.
func TestReplyRedactorHidesSharedTokens(t *testing.T) {
	sendsByType(t)
	f := newReplyFixture(t)
	quiet := after(f.ctx, commsync.Reachable{})
	open := func(node string, spec WaitReplySpec) WaitReply {
		t.Helper()
		in := f.in
		in.NodeID = node
		r, err := NewWaitReply(quiet, in, spec)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	run := open("wait", WaitReplySpec{Kind: WaitReplyWebhook, ExecutionURL: true})
	chat := open("ask", WaitReplySpec{Kind: WaitReplyFreeText, ChatSession: "session-1"})
	own := open("own", WaitReplySpec{Kind: WaitReplyFreeText})

	got := NewReplyRedactor(f.ctx, f.db).String(strings.Join([]string{run.URL, chat.Token, own.URL, own.Token}, " "))
	if strings.Contains(got, run.Token) || strings.Contains(got, chat.Token) || !strings.Contains(got, own.URL+" "+own.Token) {
		t.Errorf("redacted: %s", got)
	}
	pending, err := PendingReplies(f.ctx, f.db, "e1")
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	shown := RedactReplies(pending)
	for _, p := range shown[:2] {
		if p.URL != "" || p.Links != nil || !p.Shared || p.Outbound || !strings.Contains(p.Note, "resume URL") {
			t.Errorf("shared reply kept its link: %+v", p)
		}
	}
	if shown[2].URL != own.URL || shown[2].Shared || shown[2].Note != "" {
		t.Errorf("a node's own reply: %+v", shown[2])
	}
}
