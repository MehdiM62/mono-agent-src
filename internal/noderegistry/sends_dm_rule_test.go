package noderegistry

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// What an always-allow rule does for each DM node, pinned against the full
// schema, where queuing a card adds the recipient to People (second
// security review of #871, item 4). The rule is keyed on the recipient as
// the send addresses it, not on the person People found for it, so the next
// DM to the same chat, number or profile goes out without a card, and a DM
// to anyone else gets one. A DM whose
// node takes its recipients only from config the card can't read (an
// Instagram send_dms with a bare profile_url) names no recipient, and no
// rule ever matches it.
func TestAlwaysAllowForEachDMNode(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	Build(nil)
	run := func(context.Context, hilsend.Run) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }
	for _, c := range []struct {
		node        string
		first, same map[string]interface{}
		other       map[string]interface{}
		goesOut     bool // whether the same DM goes out once allowed
	}{
		{"comm.telegram",
			map[string]interface{}{"chat_id": "42", "message": "hello"},
			map[string]interface{}{"chat_id": "42", "message": "again"},
			map[string]interface{}{"chat_id": "7", "message": "hello"}, true},
		{"comm.twilio",
			map[string]interface{}{"operation": "send_sms", "from": "+15550002", "to": "+15550001", "body": "hi"},
			map[string]interface{}{"operation": "send_sms", "from": "+15550002", "to": "+15550001", "body": "again"},
			map[string]interface{}{"operation": "send_sms", "from": "+15550002", "to": "+15550009", "body": "hi"}, true},
		{"comm.whatsapp",
			map[string]interface{}{"operation": "send_message", "phone_number_id": "p1", "to": "+15550001", "message": "hi"},
			map[string]interface{}{"operation": "send_message", "phone_number_id": "p1", "to": "+15550001", "message": "again"},
			map[string]interface{}{"operation": "send_message", "phone_number_id": "p1", "to": "+15550009", "message": "hi"}, true},
		{"instagram.send_dms",
			map[string]interface{}{"selectedListItems": []interface{}{"https://www.instagram.com/ada/"}, "messageText": "hi"},
			map[string]interface{}{"selectedListItems": []interface{}{"https://www.instagram.com/ada/"}, "messageText": "again"},
			map[string]interface{}{"selectedListItems": []interface{}{"https://www.instagram.com/bob/"}, "messageText": "hi"}, true},
		{"instagram.send_dms (a bare profile_url)",
			map[string]interface{}{"profile_url": "https://www.instagram.com/ada/", "message": "hi"},
			map[string]interface{}{"profile_url": "https://www.instagram.com/ada/", "message": "again"},
			map[string]interface{}{"profile_url": "https://www.instagram.com/bob/", "message": "hi"}, false},
	} {
		node := c.node
		if node == "instagram.send_dms (a bare profile_url)" {
			node = "instagram.send_dms"
		}
		db := sendsDB(t)
		st := hilsend.Store{DB: db, Profile: "default"}
		// The first card adds the recipient to People, so the second card
		// already knows them as a person: the rule is made from that one.
		var out []workflow.NodeOutput
		for i := 0; i < 2; i++ {
			o, handled, err := hilsend.Guard(sendsCtx(db), node, workflow.NodeInput{}, c.first)
			if err != nil || !handled {
				t.Fatalf("%s: DM %d went out: handled=%v err=%v", c.node, i+1, handled, err)
			}
			out = o
		}
		_, err := hilsend.Resolve(context.Background(), st, out[0].Items[0].JSON["hil_id"].(string), true, hilsend.Edit{}, true, run)
		if c.goesOut && err != nil {
			t.Fatalf("%s: approve and always allow: %v", c.node, err)
		}
		if _, handled, err := hilsend.Guard(sendsCtx(db), node, workflow.NodeInput{}, c.same); err != nil || handled == c.goesOut {
			t.Errorf("%s: the same DM again: held=%v err=%v, want held=%v", c.node, handled, err, !c.goesOut)
		}
		if _, handled, err := hilsend.Guard(sendsCtx(db), node, workflow.NodeInput{}, c.other); err != nil || !handled {
			t.Errorf("%s: a DM to someone else went out: held=%v err=%v", c.node, handled, err)
		}
	}
}
