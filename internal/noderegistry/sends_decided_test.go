package noderegistry

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/workflow"
)

// notRecipients are the recipient-like keys (hilsend.RecipientLike) of
// send ops that the gate doesn't judge by name (hilsend.JudgedByKey), each
// with why it names no one the send reaches besides its recipient, by op
// ("<package> <resource>/<op> <key>", or "<node type> <op> <key>").
var notRecipients = map[string]string{
	"cisco_webex message/create destination":        "a switch between the roomId and toPerson params",
	"discord message/send sendTo":                   "a switch between the channelId and userId params",
	"facebook_graph_api /POST hostUrl":              "one of Facebook's own two Graph hosts",
	"freshservice ticket/create email":              "the requester, an address only, which the content net reads",
	"gmail_more message/send emailType":             "html or text",
	"google_calendar event/update modifyTarget":     "a recurring event's instance or its series",
	"keap email/send userId":                        "the Keap user the email is sent as",
	"mailchimp member/create groupJson":             "the member's interest groups",
	"mailchimp member/create groupsUi":              "the member's interest groups",
	"mailchimp member/update groupJson":             "the member's interest groups",
	"mattermost message/postEphemeral channelId":    "where the ephemeral post shows: only userId sees it",
	"microsoft_teams channelMessage/create teamId":  "the team holding channelId: a channel id names one channel",
	"microsoft_teams channelMessage/reply teamId":   "the team holding channelId: a channel id names one channel",
	"onfleet task/create destination":               "the street address a task delivers to",
	"onfleet worker/create teams":                   "the teams the new worker joins",
	"pager_duty incident/create email":              "the PagerDuty user the request is from (its From header)",
	"pager_duty incident/update email":              "the PagerDuty user the request is from (its From header)",
	"pager_duty incidentNote/create email":          "the PagerDuty user the request is from (its From header)",
	"plivo mms/send media_urls":                     "the media Plivo fetches and sends",
	"pushbullet push/create url":                    "the link a link push carries",
	"the_hive_project comment/add addTo":            "a switch between an alert and a case",
	"comm.telegram send_photo photo_url":            "the photo Telegram fetches and sends",
	"comm.whatsapp send_media media_url":            "the media WhatsApp fetches and sends",
	"comm.whatsapp send_template template_language": "the template's language",
	"service.devto create_comment canonical_url":    "the article's canonical link",
	"service.devto publish_article canonical_url":   "the article's canonical link",
	"service.discord send_embed avatar_url":         "the avatar the message shows",
	"service.discord send_message avatar_url":       "the avatar the message shows",
	"service.reddit comment user_agent":             "the client's User-Agent",
	"service.reddit reply_to_comment user_agent":    "the client's User-Agent",
	"service.reddit submit_post user_agent":         "the client's User-Agent",
}

// Every recipient-like param of a send op is decided (security review of
// sole-net-widen, N2): it is the op's
// recipient, text or subject param, a key the gate judges by name (a
// copy, a reply-to, the sender's, a destination or host key), a switch (a
// boolean names no one), or listed in notRecipients with why. A new
// package or param that may name someone else fails here until it is
// decided, so an always-allow rule never stands for a destination nobody
// looked at.
func TestEveryRecipientLikeKeyOfASendIsDecided(t *testing.T) {
	Build(nil)
	pkgs, err := apipkg.Load()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var undecided []string
	check := func(op, nodeType string, fields []workflow.NodeSchemaField, params ...[]string) {
		own := map[string]bool{"resource": true, "operation": true, "credential_id": true}
		for _, ps := range params {
			for _, p := range ps {
				own[p] = true
				own[p[strings.LastIndex(p, ".")+1:]] = true
			}
		}
		decide := func(k string) {
			if own[k] || !hilsend.RecipientLike(k) || hilsend.JudgedByKey(nodeType, k) {
				return
			}
			if _, ok := notRecipients[op+" "+k]; ok {
				seen[op+" "+k] = true
				return
			}
			undecided = append(undecided, op+" "+k)
		}
		for _, f := range fields {
			if f.Key == "" || f.Type == "boolean" {
				continue
			}
			decide(f.Key)
		}
	}
	for _, p := range pkgs {
		nodeType := p.NodeTypeOf()
		if p.Extends != "" {
			nodeType = p.Extends
		}
		for _, o := range p.Operations {
			s := o.Sends
			if s == nil || s.None || len(s.To) == 0 {
				continue
			}
			var visible []workflow.NodeSchemaField
			for _, f := range o.Fields {
				if len(f.VisibleFor) == 0 || visibleFor(f, o.Resource, o.Operation) {
					visible = append(visible, f)
				}
			}
			check(p.ID+" "+o.Resource+"/"+o.Operation, nodeType, visible, s.To, s.Text, s.Subject)
		}
	}
	for _, n := range hilsend.NativeSends() {
		schema, err := workflow.LoadDefaultSchema(n.NodeType)
		if err != nil {
			t.Fatalf("%s: %v", n.NodeType, err)
		}
		ops := n.Ops
		if ops == nil {
			ops = []string{""}
		}
		for _, op := range ops {
			// The op's own fields: the legacy op ("") reads the ones every
			// op shows.
			var visible []workflow.NodeSchemaField
			for _, f := range schema.Fields {
				if len(f.VisibleFor) == 0 || op != "" && visibleFor(f, "", op) {
					visible = append(visible, f)
				}
			}
			check(n.NodeType+" "+op, n.NodeType, visible, n.To, n.Text, n.Subject)
		}
	}
	sort.Strings(undecided)
	for _, u := range undecided {
		t.Errorf("%s: may name whom the send reaches; decide it: a recipient param (Sends.To), a destination key (hilsend destKeys), or notRecipients with why", u)
	}
	for k := range notRecipients {
		if !seen[k] {
			t.Errorf("notRecipients lists %q, which no send op has any more: remove it", k)
		}
	}
}

// An API package's destination keys are one more destination each: the
// URL uProc posts an SMS's delivery report to, and the chat Teams adds a
// member to, so a rule for the number or the member doesn't stand for a
// send that sets them.
func TestAnAPIPackagesDestinationKeyGetsACard(t *testing.T) {
	asOperator(t)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	Build(nil)
	for _, c := range []struct {
		nodeType, key, value string
		base                 map[string]interface{}
	}{
		{"app.uproc", "url_dlr", "https://collector.example.test/dlr",
			map[string]interface{}{"resource": "communication", "operation": "sendMobileSms", "mobile": "+15550100199", "text": "Hi"}},
		{"app.microsoft_teams", "chatId", "19:a1b2c3d4e5f6",
			map[string]interface{}{"resource": "chatMember", "operation": "add", "userId": "u-1"}},
	} {
		db := sendsDB(t)
		st := hilsend.Store{DB: db, Profile: "default"}
		held := func(cfg map[string]interface{}) (bool, hilsend.Payload) {
			out, handled, err := hilsend.Guard(sendsCtx(db), c.nodeType, workflow.NodeInput{}, cfg)
			if err != nil || !handled {
				return false, hilsend.Payload{}
			}
			p, _ := st.Get(context.Background(), out[0].Items[0].JSON["hil_id"].(string))
			return true, p
		}
		h, card := held(c.base)
		if !h || card.Recipient.Label {
			t.Fatalf("%s: not queued for a recipient: %v %+v", c.nodeType, h, card.Recipient)
		}
		if err := st.Allow(context.Background(), hilsend.Allow{Action: card.Action, Platform: card.Account.Platform, Account: card.Account.Handle, Recipient: card.Recipient.Name}); err != nil {
			t.Fatal(err)
		}
		if h, _ := held(c.base); h {
			t.Errorf("%s: the send to %s alone was held under its rule", c.nodeType, card.Recipient.Name)
		}
		cfg := cloneDeep(c.base)
		cfg[c.key] = c.value
		if h, _ := held(cfg); !h {
			t.Errorf("%s: %s went out on the rule for %s", c.nodeType, c.key, card.Recipient.Name)
		}
	}
}
