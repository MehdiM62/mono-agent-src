package hilsend

import (
	"context"
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
		{"comm.email_send", emailConfig(), "smtp.example.test", ""},
		{"comm.email_send", func() map[string]interface{} {
			c := emailConfig()
			c["smtp_port"] = float64(2525)
			return c
		}(), "smtp.example.test:2525", ""},
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
