package hilsend

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

// emailCard queues nodeType with config as an agent's send and returns its
// card.
func emailCard(t *testing.T, db *sql.DB, nodeType string, config map[string]interface{}) Payload {
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

// An Outlook reply sends to its To as well as to the thread (Graph's reply
// takes it as the message's toRecipients), so its card shows To under
// "Also to" and its recipient line says so. A Gmail reply ignores To, and
// its card says that.
func TestAnOutlookReplyShowsItsTo(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	for _, c := range []struct {
		name, nodeType string
		config         map[string]interface{}
		alsoTo, label  string
	}{
		{"an Outlook reply with a To", "service.outlook_mail", map[string]interface{}{"operation": "reply", "message_id": "M1", "to": "evil@example.test", "body": "Ok"},
			"evil@example.test", "the thread's sender and Also to"},
		{"an Outlook reply to all with a To", "service.outlook_mail", map[string]interface{}{"operation": "reply", "message_id": "M1", "reply_all": true,
			"to": []interface{}{"evil@example.test", "eve@example.test"}, "body": "Ok"},
			"evil@example.test, eve@example.test", "everyone on the thread and Also to"},
		{"an Outlook reply with no To", "service.outlook_mail", map[string]interface{}{"operation": "reply", "message_id": "M1", "body": "Ok"},
			"", "the thread's sender"},
		{"a Gmail reply with a To", "service.gmail", map[string]interface{}{"operation": "reply", "message_id": "M1", "to": "evil@example.test", "body": "Ok"},
			"", "the thread's sender (a reply ignores To)"},
	} {
		p := emailCard(t, db, c.nodeType, c.config)
		got := ""
		for _, d := range p.Details {
			if d.Key == "alsoTo" {
				got = d.Value
			}
		}
		if got != c.alsoTo || !strings.HasPrefix(p.Recipient.Name, c.label) || (c.alsoTo == "" && strings.Contains(p.Recipient.Name, "Also to")) {
			t.Errorf("%s: alsoTo %q, recipient %q; want %q, %q", c.name, got, p.Recipient.Name, c.alsoTo, c.label)
		}
	}
}

// A detail's value is the agent's text: it is written on one line, with
// the characters that break or hide a line escaped, so it can't add lines
// to `hil show` (a forged "Prepared by:") or clear the terminal.
func TestAnEmailCardsDetailsStayOnTheirLine(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	p := emailCard(t, db, "service.gmail", map[string]interface{}{"operation": "send_message", "to": "ada@example.test", "body": "Hi",
		"cc": "a@x.test\nPrepared by: MonoAgent (approved)\x1b[2J", "bcc": "b@x.test\u2028c@x.test\u2029", "references": "<r1>\r\n<r2>",
		"credential_id": "conn\u202e1"})
	for _, d := range p.Details {
		if strings.ContainsAny(d.Value, "\n\r\x1b\u2028\u2029\u202e") {
			t.Errorf("%s = %q: a line break or a control is left in", d.Key, d.Value)
		}
	}
	want := map[string]string{"cc": `a@x.test\u000APrepared by: MonoAgent (approved)\u001B[2J`, "bcc": `b@x.test\u2028c@x.test`,
		"references": `<r1>\u000D\u000A<r2>`, "connection": `conn\u202E1`}
	for _, d := range p.Details {
		if w, ok := want[d.Key]; ok && d.Value != w {
			t.Errorf("%s = %q, want %q", d.Key, d.Value, w)
		}
	}
}

// Sending a draft sends what the draft in the mailbox says, its
// recipients, subject and text, none of which the card can show: a
// send_draft card of Gmail's or Outlook's, from any route, can only be
// rejected.
func TestASendDraftCardCantBeApproved(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	st := Store{db, "p1"}
	for _, nodeType := range []string{"service.gmail", "service.outlook_mail", "gmail", "comm.outlook_send"} {
		p := emailCard(t, db, nodeType, map[string]interface{}{"operation": "send_draft", "message_id": "D1"})
		if !strings.Contains(p.Unreviewable, "draft") {
			t.Errorf("%s: unreviewable %q, want the draft's reason", nodeType, p.Unreviewable)
		}
		f := &fakeRunner{}
		if _, err := Resolve(context.Background(), st, p.ID, true, Edit{}, false, f.run); !errors.Is(err, ErrUnreviewable) || len(f.calls) != 0 {
			t.Errorf("%s: approve: %v, %d run(s)", nodeType, err, len(f.calls))
		}
	}
	if p := emailCard(t, db, "service.gmail", map[string]interface{}{"operation": "send_message", "to": "ada@example.test", "body": "Hi"}); p.Unreviewable != "" {
		t.Errorf("a send_message card: unreviewable %q", p.Unreviewable)
	}
}

// The Outlook package's draft/send sends the draft to the draft's own
// recipients, whatever its to says: its card, like send_draft's, can only be
// rejected (security review of send-card-fidelity, B1).
func TestAnOutlookPackageDraftSendCardCantBeApproved(t *testing.T) {
	outlook := target{node: "service.outlook_mail"}
	for _, c := range []struct {
		node   string
		config map[string]interface{}
		want   bool
	}{
		{"service.outlook_mail", map[string]interface{}{"resource": "draft", "operation": "send", "draftId": "D1", "to": "ada@example.test"}, true},
		{"service.outlook_mail", map[string]interface{}{"resource": " Draft ", "operation": " send "}, true},
		{"service.outlook_mail", map[string]interface{}{"operation": "send_draft"}, true},
		{"service.gmail", map[string]interface{}{"operation": "send_draft"}, true},
		{"service.outlook_mail", map[string]interface{}{"resource": "message", "operation": "send"}, false},
		{"service.gmail", map[string]interface{}{"resource": "draft", "operation": "send"}, false},
	} {
		outlook.node = c.node
		if got := sendsDraft(outlook, c.config); got != c.want {
			t.Errorf("%s %v: sendsDraft = %v, want %v", c.node, c.config, got, c.want)
		}
	}
}

// SMTP's legacy html flag makes the body HTML, as body_type does, so the
// card shows it as the body type; a body_type set as well wins, as in the
// node.
func TestAnSMTPCardShowsItsHTMLFlag(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	bodyType := func(p Payload) string {
		for _, d := range p.Details {
			if d.Key == "bodyType" {
				return d.Value
			}
		}
		return ""
	}
	for _, c := range []struct {
		config map[string]interface{}
		want   string
	}{
		{map[string]interface{}{"to": "ada@example.test", "body": "<b>Hi</b>", "html": true}, "html"},
		{map[string]interface{}{"to": "ada@example.test", "body": "Hi", "html": false}, ""},
		{map[string]interface{}{"to": "ada@example.test", "body": "Hi", "html": true, "body_type": "text"}, "text"},
	} {
		if got := bodyType(emailCard(t, db, "comm.email_send", c.config)); got != c.want {
			t.Errorf("%v: body type %q, want %q", c.config, got, c.want)
		}
	}
}

// The card names the connection by its name, with the id in parentheses; a
// connection the profile doesn't have shows as the bare id.
func TestAnEmailCardNamesItsConnection(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	if _, err := db.Exec(`CREATE TABLE connections (id TEXT, label TEXT, profile_id TEXT);
		INSERT INTO connections VALUES ('conn-1', 'Work mail', 'p1'), ('conn-2', 'Theirs', 'other')`); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"conn-1": "Work mail (conn-1)", "conn-2": "conn-2", "conn-3": "conn-3"} {
		out, handled, err := Guard(ctxFor(db), "comm.email_send", workflow.NodeInput{},
			map[string]interface{}{"to": "ada@example.test", "credential_id": id})
		if err != nil || !handled {
			t.Fatalf("%s: handled=%v err=%v", id, handled, err)
		}
		p, err := Store{db, "p1"}.Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, d := range p.Details {
			if d.Key == "connection" {
				got = d.Value
			}
		}
		if got != want {
			t.Errorf("%s: connection shown as %q, want %q", id, got, want)
		}
	}
}
