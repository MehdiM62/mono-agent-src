package hilsend

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// allowFor queues one send of nodeType with config, always-allows its card
// through Resolve as the operator would, and returns the store.
func allowFor(t *testing.T, nodeType string, config map[string]interface{}) Store {
	t.Helper()
	asAgent(t)
	return allowIn(t, testDB(t), nodeType, config)
}

// allowIn is allowFor in db.
func allowIn(t *testing.T, db *sql.DB, nodeType string, config map[string]interface{}) Store {
	t.Helper()
	st := Store{db, "p1"}
	out, handled, err := Guard(ctxFor(db), nodeType, workflow.NodeInput{}, config)
	if err != nil || !handled {
		t.Fatalf("%s: queue handled=%v err=%v", nodeType, handled, err)
	}
	if _, err := Resolve(context.Background(), st, out[0].Items[0].JSON["hil_id"].(string), true, Edit{}, true, (&fakeRunner{}).run); err != nil {
		t.Fatalf("%s: approve and always allow: %v", nodeType, err)
	}
	return st
}

func held(t *testing.T, st Store, nodeType string, config map[string]interface{}) (bool, Payload) {
	t.Helper()
	out, handled, err := Guard(ctxFor(st.DB), nodeType, workflow.NodeInput{}, config)
	if err != nil {
		t.Fatalf("%s: %v", nodeType, err)
	}
	if !handled {
		return false, Payload{}
	}
	p, _ := st.Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
	return true, p
}

func with(base map[string]interface{}, kv ...interface{}) map[string]interface{} {
	c := cloneMap(base)
	for i := 0; i+1 < len(kv); i += 2 {
		c[kv[i].(string)] = kv[i+1]
	}
	return c
}

// Gmail's and Outlook's reply answers the thread and send_draft sends the
// draft to its own recipients: neither reads to, so a rule for to never
// stands for them, and the card says who they really go to (second security
// review of #871).
func TestRepliesAndDraftsNeverMatchARule(t *testing.T) {
	for _, node := range []string{"service.gmail", "service.outlook_mail"} {
		base := map[string]interface{}{"operation": "send_message", "access_token": "x", "to": "ada@example.test", "subject": "Hi", "body": "x"}
		st := allowFor(t, node, base)
		if h, _ := held(t, st, node, base); h {
			t.Fatalf("%s: the plain send to the allowed recipient was held", node)
		}
		for name, c := range map[string]struct {
			cfg   map[string]interface{}
			label string
		}{
			"reply":                           {with(base, "operation", "reply", "message_id", "m1"), "the thread's sender"},
			"reply_all":                       {with(base, "operation", "reply", "message_id", "m1", "reply_all", true), "everyone on the thread"},
			"reply_all string":                {with(base, "operation", "reply", "message_id", "m1", "reply_all", "true"), "everyone on the thread"},
			"send_draft":                      {with(base, "operation", "send_draft", "message_id", "d1"), "the draft's own recipients"},
			"reply with no to":                {map[string]interface{}{"operation": "reply", "access_token": "x", "message_id": "m1", "body": "x"}, "the thread's sender"},
			"send_message with reply_all set": {with(base, "reply_all", true), "everyone on the thread"},
		} {
			h, p := held(t, st, node, c.cfg)
			if !h {
				t.Errorf("%s %s: went out without a card", node, name)
				continue
			}
			if !p.Recipient.Label || !strings.HasPrefix(p.Recipient.Name, c.label) {
				t.Errorf("%s %s: the card names %+v, want a label starting %q", node, name, p.Recipient, c.label)
			}
		}
	}
}

// A destination key besides the recipient (a webhook URL, a TwiML URL) is
// one more destination: a rule for the recipient doesn't stand for it.
func TestDestinationKeysBesideTheRecipientGetACard(t *testing.T) {
	for _, c := range []struct {
		node, key, value string
		base             map[string]interface{}
	}{
		{"service.discord", "webhook_url", "https://attacker.test/hook", map[string]interface{}{"operation": "send_message", "bot_token": "x", "channel_id": "123", "text": "hi"}},
		{"comm.discord", "webhook_url", "https://attacker.test/hook", map[string]interface{}{"operation": "send_message", "bot_token": "x", "channel_id": "123", "text": "hi"}},
		{"comm.twilio", "url", "https://attacker.test/twiml", map[string]interface{}{"operation": "send_sms", "from": "+15550199", "to": "+15550100", "body": "hi"}},
		{"service.reddit", "url", "https://attacker.test/x", map[string]interface{}{"operation": "submit_post", "access_token": "x", "subreddit": "golang", "title": "t", "text": "hi"}},
		{"service.outlook_mail", "message_id", "m1", map[string]interface{}{"operation": "send_message", "access_token": "x", "to": "ada@example.test", "body": "hi"}},
	} {
		st := allowFor(t, c.node, c.base)
		if h, _ := held(t, st, c.node, c.base); h {
			t.Errorf("%s: the send its rule names was held", c.node)
			continue
		}
		if h, _ := held(t, st, c.node, with(c.base, c.key, c.value)); !h {
			t.Errorf("%s with %s: went out without a card", c.node, c.key)
		}
	}
}

// No rule stands for an op with no recipient param of its own, whatever a
// fallback key (user, channel) says: its card would name a decoy while the
// node posts to its thread, article, publication or instance (third
// security review of #871).
func TestNoRuleStandsForAnOpWithNoRecipientParam(t *testing.T) {
	for _, c := range []struct {
		node string
		cfg  map[string]interface{}
	}{
		{"service.mastodon", map[string]interface{}{"operation": "publish_status", "access_token": "x", "status": "hi", "user": "@ada"}},
		{"service.bluesky", map[string]interface{}{"operation": "create_post", "identifier": "me.bsky.social", "text": "hi", "user": "ada"}},
		{"service.devto", map[string]interface{}{"operation": "create_comment", "api_key": "x", "body_markdown": "hi", "user": "ada"}},
		{"service.hashnode", map[string]interface{}{"operation": "publish_post", "token": "x", "content_markdown": "hi", "title": "t", "user": "ada"}},
		{"service.producthunt", map[string]interface{}{"operation": "create_comment", "token": "x", "body": "hi", "user": "ada"}},
		{"service.youtube", map[string]interface{}{"operation": "reply_to_comment", "access_token": "x", "text": "hi", "user": "ada"}},
	} {
		asAgent(t)
		db := testDB(t)
		st := Store{db, "p1"}
		h, p := held(t, st, c.node, c.cfg)
		if !h {
			t.Fatalf("%s: not held", c.node)
		}
		if err := st.Allow(context.Background(), Allow{Action: p.Action, Platform: p.Account.Platform, Account: p.Account.Handle, Recipient: "ada"}); err != nil {
			t.Fatal(err)
		}
		if err := st.Allow(context.Background(), Allow{Action: p.Action, Platform: p.Account.Platform, Account: p.Account.Handle, Recipient: "@ada"}); err != nil {
			t.Fatal(err)
		}
		if h, _ := held(t, st, c.node, c.cfg); !h {
			t.Errorf("%s: went out on a rule for its fallback key", c.node)
		}
	}
}

// A Discord webhook send names its webhook's host on the card, and no rule
// stands for it.
func TestADiscordWebhookShowsItsHost(t *testing.T) {
	st := allowFor(t, "service.discord", map[string]interface{}{"operation": "send_message", "bot_token": "x", "channel_id": "123", "text": "hi"})
	h, p := held(t, st, "service.discord", map[string]interface{}{"operation": "send_message", "auth": "webhook", "webhook_url": "https://attacker.test/api/webhooks/1/tok", "text": "hi"})
	if !h || p.Recipient.Name != "webhook on attacker.test" || !p.Recipient.Label {
		t.Fatalf("held=%v recipient %+v", h, p.Recipient)
	}
}

// A Twilio call dials to, but its TwiML can dial anyone: no rule stands for
// it, and the card still names the number it calls.
func TestATwilioCallNeverMatchesARule(t *testing.T) {
	sms := map[string]interface{}{"operation": "send_sms", "from": "+15550199", "to": "+15550100", "body": "hi"}
	st := allowFor(t, "comm.twilio", sms)
	if h, _ := held(t, st, "comm.twilio", sms); h {
		t.Fatal("the allowed SMS was held")
	}
	h, p := held(t, st, "comm.twilio", map[string]interface{}{"operation": "make_call", "from": "+15550199", "to": "+15550100", "twiml": "<Response><Dial>+15550666</Dial></Response>"})
	if !h || p.Recipient.Name != "+15550100" {
		t.Fatalf("held=%v recipient %+v", h, p.Recipient)
	}
}
