package noderegistry

import (
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// A saved workflow's legacy comm.* node runs as its service.* node, and the
// engine asks the guard with the type that runs it (the executor's Type).
// Each of these sends waits for approval when an agent runs it; they went
// out unchecked because only the legacy name was in the send table, or its
// legacy operation was missing.
func TestLegacyWorkflowSendsWaitAsTheEngineAsks(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	r := Build(nil)
	db := sendsDB(t)
	for _, c := range []struct {
		saved  string
		config map[string]interface{}
	}{
		{"comm.mastodon", map[string]interface{}{"operation": "create_status", "text": "Hi all"}},
		{"comm.reddit", map[string]interface{}{"operation": "reply_to_comment", "thing_id": "t1_abc", "text": "Hi Ada"}},
		{"comm.discord", map[string]interface{}{"channel_id": "C1", "content": "Hi all"}},
		{"comm.outlook_send", map[string]interface{}{"email": "me@example.test", "to": "ada@example.test", "subject": "Hello", "body": "Hi Ada"}},
	} {
		factory, ok := r.Get(c.saved)
		if !ok {
			t.Fatalf("%s is not registered", c.saved)
		}
		runs := factory().Type()
		out, handled, err := workflow.RunExecGuard(sendsCtx(db), runs, workflow.NodeInput{}, c.config)
		if !handled || err != nil || len(out) == 0 || out[0].Items[0].JSON["hil_id"] == nil {
			t.Errorf("%s (runs as %s) %v: handled=%v err=%v, want it queued", c.saved, runs, c.config["operation"], handled, err)
		}
	}
	// A legacy comm.outlook_read config names no operation either; it reads.
	factory, _ := r.Get("comm.outlook_read")
	if _, handled, err := workflow.RunExecGuard(sendsCtx(db), factory().Type(), workflow.NodeInput{},
		map[string]interface{}{"email": "me@example.test", "mailbox": "INBOX", "limit": 5}); handled {
		t.Errorf("a legacy comm.outlook_read was held (err %v)", err)
	}
}
