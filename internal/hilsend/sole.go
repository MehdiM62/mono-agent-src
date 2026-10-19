package hilsend

import (
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

// soleRecipient reports whether a send goes to exactly the one recipient
// its card names, so that an always-allow rule for that recipient may stand
// for it: one address across its recipient params, no cc, bcc or recipient
// list anywhere in its config, and no reply-to other than that address. A
// rule for ada@x must never carry a copy to anyone else (security review of
// #871): anything more gets its card.
func soleRecipient(t target, config map[string]interface{}) bool {
	to := map[string]bool{}
	for _, k := range append(append([]string{}, t.to...), toKeys...) {
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
