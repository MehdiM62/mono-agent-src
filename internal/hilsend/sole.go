package hilsend

import (
	"net/url"
	"strings"
	"unicode"
)

// copyKeys are config keys (any case, separators ignored) that add
// recipients beside the one a card names: carbon and blind copies, and
// lists of recipients.
var copyKeys = map[string]bool{
	"cc": true, "bcc": true, "ccrecipients": true, "bccrecipients": true, "ccaddresses": true, "bccaddresses": true,
	"cclist": true, "bcclist": true, "ccemails": true, "bccemails": true, "carboncopy": true, "blindcarboncopy": true,
	"recipients": true, "torecipients": true, "toaddresses": true, "tolist": true, "participants": true, "attendees": true,
}

// replyToKeys name a reply-to address, which takes the answers elsewhere.
// in_reply_to is a Message-ID, not an address, and is not one of them.
var replyToKeys = map[string]bool{"replyto": true, "replytoaddress": true, "replytoaddresses": true, "replytorecipients": true}

func keyName(k string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, k)
}

// destKeys are, per node type, the config keys besides its recipient
// params that decide where a send goes: a value there is one more
// destination, so the send gets its card (second security review of #871).
var destKeys = map[string][]string{
	"service.discord":      {"webhook_url"},
	"comm.discord":         {"webhook_url"},
	"service.mastodon":     {"instance", "instance_url", "in_reply_to_id"},
	"comm.mastodon":        {"instance", "instance_url", "in_reply_to_id"},
	"service.devto":        {"article_id"},
	"service.hashnode":     {"publication_id"},
	"service.producthunt":  {"post_id"},
	"service.youtube":      {"comment_id"},
	"service.gmail":        {"message_id"},
	"service.outlook_mail": {"message_id"},
	"comm.outlook_send":    {"message_id"},
	"comm.twilio":          {"url"},
}

// fixedRecipient is who an op that ignores its to really sends to, as the
// card names it, or "": Gmail's and Outlook's reply answers the thread
// (reply_all, everyone on it) and send_draft sends the draft to its own
// recipients. Such a send never matches an always-allow rule, whatever its
// to says.
func fixedRecipient(nodeType string, config map[string]interface{}) string {
	op := str(config["operation"])
	switch nodeType {
	case "service.gmail", "service.outlook_mail", "comm.outlook_send":
		switch {
		case truthy(config["reply_all"]):
			return "everyone on the thread (a reply ignores To)"
		case op == "reply":
			return "the thread's sender (a reply ignores To)"
		case op == "send_draft":
			return "the draft's own recipients (sending a draft ignores To)"
		}
	}
	return ""
}

// ruleless reports a send that no always-allow rule stands for although
// its card names a recipient: a Twilio call dials to, but its TwiML can
// dial anyone else.
func ruleless(nodeType string, config map[string]interface{}) bool {
	return nodeType == "comm.twilio" && str(config["operation"]) == "make_call"
}

func truthy(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s != "" && s != "false" && s != "0" && s != "no"
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return v != nil
}

// webhookHost is the host of the webhook a send posts to instead of its
// channel (Discord's auth: webhook), or "": the card names it.
func webhookHost(config map[string]interface{}) string {
	raw := str(config["webhook_url"])
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// soleRecipient reports whether a send goes to exactly the one recipient
// its card names, so that an always-allow rule for that recipient may stand
// for it: one value across its recipient params and destination keys
// (destKeys), no cc, bcc or recipient list anywhere in its config, no
// reply-to other than that address, and an op that sends to its to (not a
// reply or a draft). A rule for ada@x must never carry a copy to anyone
// else, nor stand for a webhook or a thread it doesn't name (security
// reviews of #871): anything more gets its card.
func soleRecipient(t target, config map[string]interface{}) bool {
	if t.fixed != "" || t.ruleless || truthy(config["reply_all"]) {
		return false
	}
	to := map[string]bool{}
	for _, k := range append(append(append([]string{}, t.to...), toKeys...), t.dest...) {
		for _, v := range list(valueAt(config, k)) {
			to[strings.ToLower(v)] = true
		}
	}
	if len(to) > 1 {
		return false
	}
	return !extraRecipients(config, to, 0)
}

// extraRecipients reports a cc, bcc or recipient list (in any shape: a
// string, a list, Graph-style objects), or a reply-to other than the one
// address in to, anywhere in v.
func extraRecipients(v interface{}, to map[string]bool, depth int) bool {
	if depth > 8 {
		return true // too deep to judge: the card decides
	}
	switch x := v.(type) {
	case map[string]interface{}:
		for k, sub := range x {
			name := keyName(k)
			if copyKeys[name] && present(sub) {
				return true
			}
			if replyToKeys[name] && present(sub) {
				as := list(sub)
				if len(as) == 0 {
					return true // an address in a shape list can't read
				}
				for _, a := range as {
					if !to[strings.ToLower(a)] {
						return true
					}
				}
			}
			if extraRecipients(sub, to, depth+1) {
				return true
			}
		}
	case []interface{}:
		for _, sub := range x {
			if extraRecipients(sub, to, depth+1) {
				return true
			}
		}
	}
	return false
}

// present reports a value that holds something: a non-blank string, a
// non-empty list or object, or any other non-nil value.
func present(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(x) != ""
	case []interface{}:
		return len(x) > 0
	case []string:
		return len(x) > 0
	case map[string]interface{}:
		return len(x) > 0
	}
	return true
}
