package hilsend

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// An email card shows what else the send carries besides its recipient,
// subject and text, as it will be sent: its copies (a bcc too), whom a reply
// answers and whether all of the thread, the draft a send_draft sends, the
// body type and the connection. The card used to show only the To, so a
// send to ada@ with a bcc to someone else read as a send to ada@ alone.
func TestAnEmailCardShowsWhatElseItCarries(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	card := func(nodeType string, config map[string]interface{}) Payload {
		t.Helper()
		out, handled, err := Guard(ctxFor(db), nodeType, workflow.NodeInput{}, config)
		if err != nil || !handled {
			t.Fatalf("%s: handled=%v err=%v", nodeType, handled, err)
		}
		p, err := Store{db, "p1"}.Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	details := func(p Payload) map[string]string {
		m := map[string]string{}
		for _, d := range p.Details {
			m[d.Key] = d.Value
		}
		return m
	}
	for _, c := range []struct {
		name     string
		nodeType string
		config   map[string]interface{}
		want     map[string]string
	}{
		{"a send with copies", "service.outlook_mail", map[string]interface{}{"operation": "send_message", "to": "ada@example.test",
			"cc": "eve@example.test", "bcc": []interface{}{"mallory@example.test", "trent@example.test"}, "subject": "Hi", "body": "Hello",
			"body_type": "html", "credential_id": "conn-1"},
			map[string]string{"cc": "eve@example.test", "bcc": "mallory@example.test, trent@example.test", "bodyType": "html", "connection": "conn-1"}},
		{"a reply to all with a bcc", "service.outlook_mail", map[string]interface{}{"operation": "reply", "message_id": "M1", "reply_all": true,
			"bcc": "mallory@example.test", "body": "Thanks"},
			map[string]string{"inReplyTo": "M1", "replyAll": "yes", "bcc": "mallory@example.test"}},
		{"a draft sent", "service.outlook_mail", map[string]interface{}{"operation": "send_draft", "message_id": "D1"},
			map[string]string{"draft": "D1"}},
		{"a bcc given as objects", "service.gmail", map[string]interface{}{"operation": "send_message", "to": "ada@example.test",
			"bcc": []interface{}{map[string]interface{}{"address": "mallory@example.test"}}},
			map[string]string{"bcc": `[{"address":"mallory@example.test"}]`}},
		{"an SMTP send", "comm.email_send", map[string]interface{}{"to": "ada@example.test", "cc": "eve@example.test", "in_reply_to": "<m1@x>"},
			map[string]string{"cc": "eve@example.test", "inReplyTo": "<m1@x>"}},
		{"a reply to one", "service.outlook_mail", map[string]interface{}{"operation": "reply", "message_id": "M2", "reply_all": false, "body": "Ok"},
			map[string]string{"inReplyTo": "M2"}},
	} {
		got := details(card(c.nodeType, c.config))
		if len(got) != len(c.want) {
			t.Errorf("%s: details %v, want %v", c.name, got, c.want)
			continue
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Errorf("%s: %s = %q, want %q", c.name, k, got[k], v)
			}
		}
	}
	if p := card("comm.telegram", map[string]interface{}{"chat_id": "42", "message": "Hi", "cc": "x"}); len(p.Details) != 0 {
		t.Errorf("a message that is no email: details %v", p.Details)
	}
}
