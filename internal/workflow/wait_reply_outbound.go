package workflow

import (
	"context"
	"sync/atomic"

	"github.com/monoes/mono-agent/internal/commsync"
)

// A reply a waiting node is answered through (a sendAndWait message's links,
// core.wait's resume URL or form, a form page) can let a message to a person
// go out: the nodes after it run once it is answered. Such a reply is marked
// (WaitReplySpec.Outbound) when it opens, and the views an agent reaches
// (`workflow execution` from anything but the user's own terminal, the MCP
// server's workflow_status) leave its links out (RedactReplies), as the
// Human-in-Loop item in front of a send is the user's to decide. The public
// link itself keeps working: the person the message went to answers there.

// SendClassifier reports whether running a node of nodeType with config may
// send a message to a person; hilsend.MaySend is the one installed.
type SendClassifier func(nodeType string, config map[string]interface{}) bool

var sendClassifier atomic.Pointer[SendClassifier]

// SetSendClassifier installs c for the process (internal/noderegistry does);
// nil removes it.
func SetSendClassifier(c SendClassifier) {
	if c == nil {
		sendClassifier.Store(nil)
		return
	}
	sendClassifier.Store(&c)
}

type runsAfterKey struct{}

// withRunsAfter attaches to a node's ctx what runs after it, computed only
// when the node asks (it opens a reply).
func withRunsAfter(ctx context.Context, after func() commsync.Reachable) context.Context {
	return context.WithValue(ctx, runsAfterKey{}, after)
}

// messagesAfter reports whether anything that runs after the node of ctx
// may send a message to a person, or that can't be told: no classifier
// installed, or a called workflow it can't resolve (it fails closed). A node
// run outside an engine run (`node run`) has nothing after it.
func messagesAfter(ctx context.Context) bool {
	after, ok := ctx.Value(runsAfterKey{}).(func() commsync.Reachable)
	if !ok {
		return false
	}
	r := after()
	c := sendClassifier.Load()
	if r.Unresolved || c == nil {
		return true
	}
	for _, n := range r.Nodes {
		if (*c)(n.Type, n.Config) {
			return true
		}
	}
	return false
}

// replyOutbound is spec's recorded decision; a reply opened before it was
// recorded counts as one that lets a message to a person go out.
func replyOutbound(spec WaitReplySpec) bool {
	return spec.Outbound == nil || *spec.Outbound
}

// replyShared reports whether spec's token is shared by other replies: an
// execution's resume URL answers every core.wait and form step of the run,
// a chat session's token every reply in the session. One opened later may
// let a message go out, whatever this one's decision, so the token is kept
// from agents like a marked reply's.
func replyShared(spec WaitReplySpec) bool {
	return spec.ExecutionURL || spec.ChatSession != ""
}

// replyHiddenNote stands in for a marked reply's links, replySharedNote for
// the links of a reply whose token other replies share.
const (
	replyHiddenNote = "answering it lets a message to a person go out, so its links are the user's: the person it was sent to answers through them, and the user sees them in their own terminal or the app"
	replySharedNote = "its link is the run's resume URL (or the chat session's), which answers every reply of the run or session, one of which may let a message to a person go out, so it is the user's: they see it in their own terminal or the app"
)

// RedactReplies leaves out the links of every reply that lets a message to a
// person go out, or whose token other replies share, for a view an agent
// reaches. Others are kept as they are.
func RedactReplies(replies []PendingReply) []PendingReply {
	out := make([]PendingReply, len(replies))
	for i, p := range replies {
		switch {
		case p.Outbound:
			p.URL, p.Links, p.Note = "", nil, replyHiddenNote
		case p.Shared:
			p.URL, p.Links, p.Note = "", nil, replySharedNote
		}
		out[i] = p
	}
	return out
}
