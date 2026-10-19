package hilsend

import (
	"context"
	"net/url"
	"strings"

	"github.com/monoes/mono-agent/internal/connections"
)

// destKeys are, per node type (the canonical type that runs it: Guard
// resolves legacy names first), the config keys besides its recipient
// params that decide where a send goes: a value there is one more
// destination, so the send gets its card (second security review of #871).
var destKeys = map[string][]string{
	"service.discord":      {"webhook_url"},
	"service.mastodon":     {"instance", "instance_url", "in_reply_to_id", "status_id"},
	"service.bluesky":      {"uri", "post_uri", "cid"},
	"service.reddit":       {"url"},
	"service.devto":        {"article_id"},
	"service.hashnode":     {"publication_id", "publication_host"},
	"service.producthunt":  {"post_id"},
	"service.youtube":      {"comment_id"},
	"service.gmail":        {"message_id"},
	"service.outlook_mail": {"message_id"},
	"comm.twilio":          {"url"},
}

// hostKeys are, per node type, the keys that name the server a send goes
// through. A send through a server other than its saved connection's is
// one more destination: it would take the message, and the connection's
// password with it.
var hostKeys = map[string][]string{"comm.email_send": {"smtp_host"}}

// ownServer reports whether every host key of nodeType that config sets
// names the server of the saved connection its credential_id resolves to,
// under st's profile, as the run resolves it. The "credential" map in the
// config proves nothing (whoever writes the config can write it too): a
// host with no saved connection behind it is not the send's own.
func ownServer(ctx context.Context, st Store, nodeType string, config map[string]interface{}) bool {
	var saved *connections.Connection
	for _, k := range hostKeys[nodeType] {
		v := str(config[k])
		if v == "" {
			continue
		}
		if saved == nil {
			id := str(config["credential_id"])
			if id == "" {
				return false
			}
			conn, err := connections.NewStore(st.DB).GetOrResolve(ctx, id, st.Profile)
			if err != nil || conn == nil {
				return false
			}
			saved = conn
		}
		if !strings.EqualFold(v, str(saved.Data[k])) {
			return false
		}
	}
	return true
}

// fixedRecipient is who an op that ignores its to really sends to, as the
// card names it, or "": Gmail's and Outlook's reply answers the thread
// (reply_all, everyone on it) and send_draft sends the draft to its own
// recipients. Such a send never matches an always-allow rule, whatever its
// to says.
func fixedRecipient(nodeType string, config map[string]interface{}) string {
	op := str(config["operation"])
	switch nodeType {
	case "service.gmail", "service.outlook_mail":
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
// for it (security reviews of #871): anything more gets its card. It needs
// all of:
//   - an op that sends to a recipient param of its own (not a reply, a
//     draft, a call its TwiML directs, or an op with no recipient param);
//   - one value across its recipient params and destination keys
//     (destKeys), through its saved connection's own server (hostKeys);
//   - no cc, bcc or recipient list under any key, and no reply-to other
//     than that recipient (extraRecipients);
//   - and, the net under all of these, no address or phone number anywhere
//     else in the config but its text, its sender and that recipient
//     (strayAddress).
func soleRecipient(ctx context.Context, st Store, t target, nodeType string, config map[string]interface{}) bool {
	if t.fixed != "" || t.ruleless || truthy(config["reply_all"]) || (len(t.to) == 0 && !t.social) {
		return false
	}
	to := map[string]bool{}
	own := map[string]bool{} // the recipient params' paths, never a copy themselves
	for _, k := range append(append(append([]string{}, t.to...), toKeys...), t.dest...) {
		own[pathKey("", k)] = true
		for _, v := range list(valueAt(config, k)) {
			to[strings.ToLower(v)] = true
		}
	}
	if len(to) > 1 || !ownServer(ctx, st, nodeType, config) {
		return false
	}
	return !extraRecipients(config, "", to, own, 0) && !strayAddress(t, nodeType, config, to)
}

// extraRecipients reports a cc, bcc or recipient list (in any shape: a
// string, a list, Graph-style objects), or a reply-to other than the one
// address in to, anywhere in v (at path). own are the paths of the op's
// recipient params (AWS SES's toAddresses, Freshdesk's options.ccEmails),
// counted in to already.
func extraRecipients(v interface{}, path string, to, own map[string]bool, depth int) bool {
	if depth > 8 {
		return true // too deep to judge: the card decides
	}
	switch x := v.(type) {
	case map[string]interface{}:
		for k, sub := range x {
			at := pathKey(path, k)
			if own[at] {
				continue
			}
			if copyKey(k) && present(sub) {
				return true
			}
			if replyToKey(k) && present(sub) {
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
			if extraRecipients(sub, at, to, own, depth+1) {
				return true
			}
		}
	case []interface{}:
		for _, sub := range x {
			if extraRecipients(sub, path, to, own, depth+1) {
				return true
			}
		}
	}
	return false
}

// pathKey is the dotted path of key k under path, each part as its words
// joined (options.ccEmails and Options.cc_emails are one path).
func pathKey(path, k string) string {
	parts := strings.Split(k, ".")
	for i, p := range parts {
		parts[i] = joined(p)
	}
	if path == "" {
		return strings.Join(parts, ".")
	}
	return path + "." + strings.Join(parts, ".")
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
