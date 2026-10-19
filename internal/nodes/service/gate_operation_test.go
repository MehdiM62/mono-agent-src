package service

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// trickyOperations are "operation" values of every JSON type, and strings
// near a send's name.
var trickyOperations = []interface{}{
	nil, true, false, float64(0), float64(5), "", " ", "5", "send_message", "send_message ", "SEND_MESSAGE",
	[]interface{}{}, []interface{}{"send_message"}, map[string]interface{}{"operation": "send_message"},
}

// Whatever its "operation" holds, a config the node sends is one the send
// gate holds: Discord, and Outlook with an app password, send for an
// operation that is no string (they read it as blank, and a blank one
// sends). The gate used to render 5 or true as text and let them through.
func TestTheSendGateHoldsWhatDiscordAndOutlookSend(t *testing.T) {
	srv := withLegacyDiscord(t)
	_, smtp := outlookTransportRecorder(t)
	ctx := context.Background()
	for _, op := range trickyOperations {
		discord := map[string]interface{}{"bot_token": "abc", "channel_id": "C1", "content": "hi", "operation": op}
		srv.mu.Lock()
		before := len(srv.reqs)
		srv.mu.Unlock()
		_, _ = (&DiscordNode{}).Execute(ctx, workflow.NodeInput{}, discord)
		srv.mu.Lock()
		sent := false
		for _, r := range srv.reqs[before:] {
			sent = sent || (r.Method == "POST" && r.Path == "/channels/C1/messages")
		}
		srv.mu.Unlock()
		for _, name := range []string{"service.discord", "comm.discord", "discord"} {
			if sent && !hilsend.IsOutbound(name, discord) {
				t.Errorf("%s with operation %#v sends, and the gate let it through", name, op)
			}
		}

		outlook := map[string]interface{}{"email": "me@outlook.com", "app_password": "pw", "to": "ada@example.test",
			"subject": "Hello", "body": "Hi", "operation": op}
		before = len(*smtp)
		_, _ = (&OutlookMailNode{}).Execute(ctx, workflow.NodeInput{}, outlook)
		sent = len(*smtp) > before
		for _, name := range []string{"service.outlook_mail", "comm.outlook_send"} {
			if sent && !hilsend.IsOutbound(name, outlook) {
				t.Errorf("%s with operation %#v sends, and the gate let it through", name, op)
			}
		}
	}
}
