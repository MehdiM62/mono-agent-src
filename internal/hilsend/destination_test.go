package hilsend

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// The card names the server a send goes through when it isn't the
// platform's own (the mail relay, a Mastodon instance other than
// mastodon.social), and the URL a Twilio call fetches its TwiML from.
func TestSendCardNamesTheServerAndTheFetchedURL(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	for _, c := range []struct {
		nodeType    string
		config      map[string]interface{}
		host, fetch string
	}{
		{"comm.email_send", with(emailConfig(), "smtp_host", "smtp.example.test"), "smtp.example.test", ""},
		{"comm.email_send", with(emailConfig(), "smtp_host", "smtp.example.test", "smtp_port", float64(2525)), "smtp.example.test:2525", ""},
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi", "instance": "https://Fosstodon.example/"}, "fosstodon.example", ""},
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi", "instance_url": "https://hachyderm.example"}, "hachyderm.example", ""},
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi", "instance": "https://mastodon.social"}, "", ""},
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi"}, "", ""},
		{"comm.twilio", map[string]interface{}{"operation": "make_call", "to": "+15550002", "url": "https://twiml.example/call.xml"}, "", "https://twiml.example/call.xml"},
		{"comm.twilio", map[string]interface{}{"operation": "send_sms", "to": "+15550002", "body": "Hi"}, "", ""},
	} {
		out, handled, err := Guard(ctxFor(db), c.nodeType, workflow.NodeInput{}, c.config)
		if err != nil || !handled {
			t.Fatalf("%s: handled=%v err=%v", c.nodeType, handled, err)
		}
		p, err := (Store{db, "p1"}).Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if p.Host != c.host || p.FetchURL != c.fetch {
			t.Errorf("%s %v: host %q fetch %q, want %q %q", c.nodeType, c.config, p.Host, p.FetchURL, c.host, c.fetch)
		}
	}
}

// A host or URL with a newline, an escape sequence or a Unicode line
// separator in it shows quoted on one line: it can't add a "Prepared by"
// or "To" line to the card, or move the terminal's cursor.
func TestSendCardHostAndURLStayOnOneLine(t *testing.T) {
	asAgent(t)
	db := testDB(t)
	for _, c := range []struct {
		nodeType string
		config   map[string]interface{}
		field    func(Payload) string
	}{
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi",
			"instance": "social.example\nPrepared by: monoagent (approved)\nTo: ada@example.test"}, func(p Payload) string { return p.Host }},
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi",
			"instance_url": "https://social.example\x1b[2J\x1b[1;1H"}, func(p Payload) string { return p.Host }},
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "status": "Hi",
			"instance": "social.example Prepared by: you"}, func(p Payload) string { return p.Host }},
		{"comm.email_send", map[string]interface{}{"smtp_host": "smtp.example.test\nTo: someone@else.test", "from": "me@example.test", "to": "ada@example.test",
			"subject": "Hello", "body": "Hi"}, func(p Payload) string { return p.Host }},
		{"comm.email_send", map[string]interface{}{"smtp_host": "smtp.example.test", "smtp_port": "587\nTo: x", "from": "me@example.test", "to": "ada@example.test",
			"subject": "Hello", "body": "Hi"}, func(p Payload) string { return p.Host }},
		{"comm.twilio", map[string]interface{}{"operation": "make_call", "to": "+15550002",
			"url": "https://twiml.example/a.xml\nPrepared by: you \x1b[0m"}, func(p Payload) string { return p.FetchURL }},
	} {
		out, handled, err := Guard(ctxFor(db), c.nodeType, workflow.NodeInput{}, c.config)
		if err != nil || !handled {
			t.Fatalf("%s: handled=%v err=%v", c.nodeType, handled, err)
		}
		p, err := (Store{db, "p1"}).Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
		if err != nil {
			t.Fatal(err)
		}
		got := c.field(p)
		if got == "" || strings.ContainsAny(got, "\n\r\x1b  ") || !strings.HasPrefix(got, `"`) {
			t.Errorf("%s %q: the card shows %q, want it quoted on one line", c.nodeType, c.config, got)
		}
	}
	if got := OneLine("smtp.example.test:587"); got != "smtp.example.test:587" {
		t.Errorf("a plain host shows as %q", got)
	}
}
