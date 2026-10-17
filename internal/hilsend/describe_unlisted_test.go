package hilsend

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Sends the classification missed: a Twilio voice call reads a message to a
// person, and service.discord's embed posts one to a channel (comm.discord's
// rule had it, service.discord's did not). Both queue like the other sends
// (#831 follow-up, item 4).
func TestUnlistedSendsAreGated(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	for _, c := range []struct {
		node, text, to string
		cfg            map[string]interface{}
	}{
		{"comm.twilio", "<Response><Say>Hi Ada</Say></Response>", "+15550100",
			map[string]interface{}{"operation": "make_call", "from": "+15550199", "to": "+15550100", "twiml": "<Response><Say>Hi Ada</Say></Response>"}},
		{"service.discord", "Launch today", "123",
			map[string]interface{}{"operation": "send_embed", "channel_id": "123", "embed_title": "News", "embed_description": "Launch today"}},
	} {
		out, handled, err := Guard(ctxFor(db), c.node, workflow.NodeInput{}, c.cfg)
		if !handled || err != nil {
			t.Fatalf("%s %s: handled=%v err=%v", c.node, c.cfg["operation"], handled, err)
		}
		p, err := (Store{db, "p1"}).Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
		if err != nil || p.Text != c.text || p.Recipient.Name != c.to {
			t.Fatalf("%s: item %+v, %v", c.node, p, err)
		}
	}
	for _, c := range []struct {
		node string
		cfg  map[string]interface{}
	}{
		{"service.discord", map[string]interface{}{"operation": "list_messages", "channel_id": "123"}},
		{"comm.twilio", map[string]interface{}{"operation": "get_messages"}},
	} {
		if _, handled, _ := Guard(ctxFor(db), c.node, workflow.NodeInput{}, c.cfg); handled {
			t.Errorf("%s %s was held, but it sends nothing", c.node, c.cfg["operation"])
		}
	}
}
