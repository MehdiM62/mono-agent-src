package hilsend

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// keyWords splits a config key into its lowercase words: at separators and
// where the case turns (ccEmail is cc and email, bcc_address is bcc and
// address, CCAddress is cc and address). Width variants are folded and
// invisible characters dropped first, so a fullwidth or zero-width-split
// key reads as the key it imitates.
func keyWords(k string) []string {
	k = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, norm.NFKC.String(k))
	var words []string
	var cur []rune
	rs := []rune(k)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = nil
		}
	}
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if i > 0 && unicode.IsUpper(r) {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

func joined(k string) string { return strings.Join(keyWords(k), "") }

// listKeys name lists of recipients (any case, separators ignored).
var listKeys = map[string]bool{
	"recipients": true, "torecipients": true, "toaddresses": true, "tolist": true, "participants": true,
	"attendees": true, "additionalemails": true, "carboncopy": true, "blindcarboncopy": true,
}

// copyKey reports a key that adds recipients beside the card's one: any key
// with a word cc or bcc (cc, Bcc, ccEmail, bcc_address, ccRecipients,
// options.bcc_address at any depth), or a list of recipients
// (additional_emails, attendees).
func copyKey(k string) bool {
	ws := keyWords(k)
	return slices.Contains(ws, "cc") || slices.Contains(ws, "bcc") || listKeys[strings.Join(ws, "")]
}

// replyToKey reports a reply-to address, which takes the answers
// elsewhere. in_reply_to is a Message-ID, not an address.
func replyToKey(k string) bool {
	switch joined(k) {
	case "replyto", "replytoaddress", "replytoaddresses", "replytorecipients", "replytoemail":
		return true
	}
	return false
}

// proseKeys are the fields that hold what a send says rather than where it
// goes, besides each op's own text and subject params: an address written
// in them is prose.
var proseKeys = []string{"subject", "body", "text", "html", "message", "content", "title", "description"}

// senderWords mark a key that names the sending side (the account, its
// login, its address) or a credential, never a recipient.
var senderWords = []string{"from", "sender", "account", "username", "login", "identifier", "author", "owner", "mailbox", "credential", "credentials", "password", "token", "secret"}

// senderKeys are, per node type, more top-level keys that name the sending
// side: Outlook's app-password login.
var senderKeys = map[string][]string{"service.outlook_mail": {"email"}, "comm.outlook_send": {"email"}}

// threadKeys name a message or thread id (a Message-ID looks like an
// address).
var threadKeys = map[string]bool{"inreplyto": true, "references": true, "messageid": true, "threadid": true}

// SenderKey reports whether the top-level key, in a config of nodeType,
// names the sending side, a credential or a thread rather than anywhere a
// send goes: the content net (strayAddress) reads past it.
func SenderKey(nodeType, key string) bool {
	j := joined(key)
	return senderWord(key) || slices.ContainsFunc(senderKeys[nodeType], func(s string) bool { return joined(s) == j })
}

// senderWord reports a key, at any depth, with a sender or credential word,
// or a message or thread id.
func senderWord(key string) bool {
	ws := keyWords(key)
	for _, w := range senderWords {
		if slices.Contains(ws, w) {
			return true
		}
	}
	return threadKeys[strings.Join(ws, "")]
}

// addressRE finds an email address or a +phone number.
var addressRE = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}|\+[0-9][0-9 ().-]{6,}[0-9]`)

// addressKey is an address as compared: an email lowercased, a phone
// number as + and its digits.
func addressKey(a string) string {
	if strings.Contains(a, "@") {
		return strings.ToLower(a)
	}
	return "+" + strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, a)
}

// strayAddress is the net under the key rules: it reports an address or a
// phone number in any string of the config but the op's text (its text and
// subject params, and proseKeys), its sender (SenderKey) and the one
// recipient in to. Whatever key a node reads a copy from, a rule never
// stands for a send that names anyone else.
func strayAddress(t target, nodeType string, config map[string]interface{}, to map[string]bool) bool {
	own := map[string]bool{}
	for v := range to {
		for _, a := range addressRE.FindAllString(v, -1) {
			own[addressKey(a)] = true
		}
	}
	prose := map[string]bool{}
	for _, k := range append(append(append([]string{}, t.text...), t.subject...), proseKeys...) {
		prose[joined(k)] = true
		if _, leaf, dotted := strings.Cut(k, "."); dotted {
			prose[joined(leaf)] = true
		}
	}
	var walk func(v interface{}, key string, depth int) bool
	walk = func(v interface{}, key string, depth int) bool {
		if depth > 8 {
			return true // too deep to judge: the card decides
		}
		switch x := v.(type) {
		case string:
			for _, a := range addressRE.FindAllString(x, -1) {
				if !own[addressKey(a)] {
					return true
				}
			}
		case []string:
			for _, s := range x {
				if walk(s, key, depth+1) {
					return true
				}
			}
		case []interface{}:
			for _, s := range x {
				if walk(s, key, depth+1) {
					return true
				}
			}
		case map[string]interface{}:
			for k, sub := range x {
				if prose[joined(k)] || senderWord(k) || (depth == 0 && SenderKey(nodeType, k)) {
					continue
				}
				if walk(sub, k, depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(config, "", 0)
}
