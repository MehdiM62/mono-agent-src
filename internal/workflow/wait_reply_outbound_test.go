package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/commsync"
)

// sendsByType stands in for hilsend.MaySend: only comm.email_send sends.
func sendsByType(t *testing.T) {
	t.Helper()
	old := sendClassifier.Load()
	SetSendClassifier(func(nodeType string, _ map[string]interface{}) bool { return nodeType == "comm.email_send" })
	t.Cleanup(func() { sendClassifier.Store(old) })
}

func after(ctx context.Context, r commsync.Reachable) context.Context {
	return withRunsAfter(ctx, func() commsync.Reachable { return r })
}

// What runs after a node decides whether its reply lets a message go out;
// what can't be told counts as sending.
func TestMessagesAfter(t *testing.T) {
	sendsByType(t)
	ctx := context.Background()
	mail := commsync.Reachable{Nodes: []commsync.Node{{Type: "transform.set"}, {Type: "comm.email_send"}}}
	set := commsync.Reachable{Nodes: []commsync.Node{{Type: "transform.set"}}}
	for name, tc := range map[string]struct {
		ctx  context.Context
		want bool
	}{
		"outside a run":         {ctx, false},
		"an email further down": {after(ctx, mail), true},
		"nothing sends":         {after(ctx, set), false},
		"unresolved":            {after(ctx, commsync.Reachable{Nodes: set.Nodes, Unresolved: true}), true},
	} {
		if got := messagesAfter(tc.ctx); got != tc.want {
			t.Errorf("%s: messagesAfter = %v, want %v", name, got, tc.want)
		}
	}
	SetSendClassifier(nil)
	if !messagesAfter(after(ctx, set)) {
		t.Error("no classifier installed: not failing closed")
	}
}

// A reply opened in front of a send is marked and its links stay out of a
// view an agent reaches; one in front of nothing that sends keeps them.
func TestWaitReplyInFrontOfASendIsTheUsers(t *testing.T) {
	sendsByType(t)
	f := newReplyFixture(t)
	mail := commsync.Reachable{Nodes: []commsync.Node{{Type: "comm.email_send"}}}
	if _, err := NewWaitReply(after(f.ctx, mail), f.in, WaitReplySpec{Kind: WaitReplyApproval, ApprovalType: "double"}); err != nil {
		t.Fatal(err)
	}
	quiet := f.in
	quiet.NodeID = "n2"
	if _, err := NewWaitReply(after(f.ctx, commsync.Reachable{}), quiet, WaitReplySpec{Kind: WaitReplyWebhook}); err != nil {
		t.Fatal(err)
	}
	pending, err := PendingReplies(f.ctx, f.db, "e1")
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	if !pending[0].Outbound || pending[1].Outbound {
		t.Fatalf("outbound marks = %v, %v", pending[0].Outbound, pending[1].Outbound)
	}
	shown := RedactReplies(pending)
	if shown[0].URL != "" || shown[0].Links != nil || !strings.Contains(shown[0].Note, "message to a person") {
		t.Errorf("the send's reply kept its links: %+v", shown[0])
	}
	if shown[1].URL == "" || shown[1].Note != "" {
		t.Errorf("the plain reply lost its link: %+v", shown[1])
	}
	if pending[0].URL == "" {
		t.Error("RedactReplies changed its argument")
	}
}
