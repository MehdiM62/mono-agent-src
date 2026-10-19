package hilsend

import (
	"encoding/json"
	"strings"
)

// Detail is one more thing an email send carries that the card's
// recipient, subject and text don't show, as the send will carry it.
type Detail struct {
	// Key names it: alsoTo (an Outlook reply's To, sent to with the
	// thread), cc, bcc, replyAll, inReplyTo (the message a reply answers),
	// draft (the draft sending sends), references, bodyType or connection
	// (the saved connection it goes out through).
	Key   string `json:"key"`
	Value string `json:"value"`
}

// emailDetails are the config keys an email node reads that the card shows
// as details, by Detail key, in the card's order.
var emailDetails = []struct {
	key  string
	from string
}{
	{"cc", "cc"},
	{"bcc", "bcc"},
	{"replyAll", "reply_all"},
	{"inReplyTo", "message_id"},
	{"inReplyTo", "in_reply_to"},
	{"references", "references"},
	{"bodyType", "body_type"},
	{"bodyType", "html"}, // SMTP's legacy flag, when body_type isn't set
	{"connection", "credential_id"},
}

// detailsOf is what an email send's card shows besides its recipient,
// subject and text: an Outlook reply's To, every copy (cc, bcc), whom a
// reply answers and whether all of the thread, the draft a send_draft
// sends, the body type and the connection. A send that is no email has
// none. Each value is the step's text on one line, with what breaks or
// hides a line escaped (escapeHidden), so it can't add lines to `hil show`.
func detailsOf(t target, config map[string]interface{}) []Detail {
	if t.action != ActionEmail {
		return nil
	}
	var out []Detail
	if t.node == "service.outlook_mail" && opOf(config) == "reply" {
		if to := detailValue(config["to"]); to != "" {
			out = append(out, Detail{Key: "alsoTo", Value: escapeHidden(to)})
		}
	}
	for _, d := range emailDetails {
		v, ok := config[d.from]
		if !ok {
			continue
		}
		value := detailValue(v)
		if d.from == "html" {
			// The flag counts only when no body_type is set, as in the node.
			if value == "" || detailValue(config["body_type"]) != "" {
				continue
			}
			value = "html"
		}
		if value == "" {
			continue
		}
		key := d.key
		if key == "inReplyTo" && opOf(config) == "send_draft" {
			key = "draft"
		}
		out = append(out, Detail{Key: key, Value: escapeHidden(value)})
	}
	return out
}

// detailValue is v as the card shows it: a string as is, a list of strings
// joined with ", ", true as yes, anything else (a list with an object in it
// too) as JSON, so no address in it is left out; "" for nothing and false.
func detailValue(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		if x {
			return "yes"
		}
		return ""
	case string:
		return strings.TrimSpace(x)
	case []interface{}:
		if l := list(x); len(l) == len(x) {
			return strings.Join(l, ", ")
		}
	case []string:
		return strings.Join(list(x), ", ")
	}
	b, _ := json.Marshal(v)
	if s := string(b); s != "[]" && s != "{}" {
		return s
	}
	return ""
}

// draftUnreviewable is why a send_draft card can't be approved
// (Payload.Unreviewable).
const draftUnreviewable = "sending a draft sends the recipients, subject and text the draft holds in the mailbox, which this card can't show: " +
	"reject it, and send the draft from the mailbox after reading it"

// sendsDraft reports a Gmail or Outlook send_draft: what it sends is the
// draft as the mailbox holds it, none of which the card shows.
func sendsDraft(t target, config map[string]interface{}) bool {
	return (t.node == "service.gmail" || t.node == "service.outlook_mail") && opOf(config) == "send_draft"
}
