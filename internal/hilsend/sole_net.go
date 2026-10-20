package hilsend

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// keyWords splits a config key into its lowercase words: at separators and
// where the case turns (ccEmail is cc and email, bcc_address is bcc and
// address, CCAddress is cc and address). Width variants are folded and
// invisible characters dropped first, so a fullwidth or zero-width-split
// key reads as the key it imitates.
func keyWords(k string) []string {
	k = foldWidth(k)
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

// foldWidth is s with its width variants folded (a fullwidth CC or at
// sign reads as the ASCII one) and its invisible characters dropped.
func foldWidth(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, norm.NFKC.String(s))
}

// listKeys name lists of recipients (any case, separators ignored).
var listKeys = map[string]bool{
	"recipients": true, "torecipients": true, "toaddresses": true, "tolist": true, "participants": true,
	"attendees": true, "additionalemails": true, "carboncopy": true, "blindcarboncopy": true,
}

// copyWords mark a key that adds recipients beside the card's one: a cc or
// a bcc (ccs, bccs), an issue's watchers, and recipients of any kind
// (Jira's notificationRecipientsUi, whose users and groups are account
// ids), whatever their value (an address, or an id the net can't read as
// one).
var copyWords = []string{"cc", "ccs", "bcc", "bccs", "watcher", "watchers", "recipients"}

// copyKey reports a key that adds recipients beside the card's one: any key
// with a copy word (cc, Bcc, ccEmail, bcc_address, ccRecipients, ccs,
// watcherIds, notificationRecipientsUi, options.bcc_address at any depth),
// or a list of recipients (additional_emails, attendees).
func copyKey(k string) bool {
	ws := keyWords(k)
	return slices.ContainsFunc(ws, func(w string) bool { return slices.Contains(copyWords, w) }) || listKeys[strings.Join(ws, "")]
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

// senderKeys are, per (canonical) node type, more top-level keys that name
// the sending side: Outlook's app-password login, and the WhatsApp
// business number a message goes out from.
var senderKeys = map[string][]string{"service.outlook_mail": {"email"}, "comm.whatsapp": {"phone_number_id"}}

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

// netAddresses calls f with each email address and phone number in s, as
// the content net compares them, and stops when f returns false (reporting
// false). It reads s as a mail or phone app might: width variants folded
// (a fullwidth at sign), %XX escapes decoded (evil%40x.test), and the
// punctuation that would split a word at an @ dropped (atJoined:
// "evil"@x.test, evil(c)@x.test, user@[10.0.0.1]). An address is a word
// scanAddresses finds with an @ inside it (a Unicode local part or domain,
// a domain with no dot), lowercased, its last @ splitting it, its local
// part starting after a link's punctuation (mailto:ada@…, ?to=ada@…) and
// its domain ending before a path or query. A phone number is a number
// numberTokens reads (+1 555 010 0199, 0044…, 5550100199, an id of as many
// digits), as its digits and its +, unless its digits are glued after a
// letter (INC0012345, a tracking number's 1Z999AA1…; idDigits). Links,
// handles, channels, UUIDs and IP literals are no one's address here:
// destKeys and hostKeys judge where a send goes.
//
// It doesn't see (security review of sole-net-widen, N2 and N3): what has
// no address's shape, as an @handle, a Matrix or Slack id, a UUID or an
// account id (the key rules judge those, and every recipient-like key of a
// send op is decided: TestEveryRecipientLikeKeyOfASendIsDecided); an
// address spelled out ("evil at attacker dot test"), in base64 or as an
// HTML entity (&#64;), or with a space before its @ (a handle then); a
// number a config holds as a JSON number; and a phone number split into
// single digits, by / or _, or by 4 spaces or more, or one under 7 digits
// (a short code).
func netAddresses(s string, f func(string) bool) bool {
	s = foldWidth(unescapePercent(s))
	if !brokenAddresses(s, f) {
		return false
	}
	s = atJoined(s)
	phone := func(t string) bool {
		if d := strings.TrimPrefix(t, "+"); len(d) >= 7 && len(d) <= 20 && strings.Trim(d, "0123456789") == "" {
			return f(t)
		}
		return true // an IPv4 literal
	}
	if !numberTokens(idDigits(s), phone) {
		return false
	}
	return scanAddresses(s, func(t string) bool {
		at := strings.LastIndexByte(t, '@')
		if at <= 0 {
			return true // a number (read above), a handle, a link…
		}
		local := t[strings.LastIndexAny(t[:at], "/:?=&")+1 : at]
		domain := t[at+1:]
		if i := strings.IndexAny(domain, "/?#&"); i >= 0 {
			domain = domain[:i]
		}
		if local == "" || domain == "" {
			return true
		}
		return f(strings.ToLower(local + "@" + domain))
	})
}

// brokenAddresses calls f with each space-free word of s that has an @ and,
// once the break characters around it (<ada@x.test>, "ada@x.test", a
// trailing comma) are trimmed, a break character inside it: evil|ada@x.test,
// evil(ada)@x.test, ada"@x.test (from "evil ada"@x.test). A mail app reads
// those as one mailbox, whose local part ends where the allowed recipient's
// begins, so the word counts as an address of its own, whole. It stops when
// f returns false (reporting false).
func brokenAddresses(s string, f func(string) bool) bool {
	for _, w := range strings.FieldsFunc(s, unicode.IsSpace) {
		if !strings.Contains(w, "@") {
			continue
		}
		if inner := strings.Trim(w, atBreaks); strings.ContainsAny(inner, atBreaks) && !f(strings.ToLower(inner)) {
			return false
		}
	}
	return true
}

// atBreaks are the characters besides spaces that scanAddresses ends a
// word at: next to an @, they would hide an address ("evil"@x.test reads
// as the handle @x.test).
const atBreaks = ",;\"'<>()[]{}|\\`"

// atJoined is s with each run of atBreaks next to an @ dropped, on either
// side: "evil"@x.test reads as evil@x.test, evil(c)@x.test as c@x.test,
// evil@(c)x.test as evil@c, user@[10.0.0.1] as user@10.0.0.1. Whatever it
// reads is an address, and one that isn't the recipient's gets the card.
func atJoined(s string) string {
	if !strings.Contains(s, "@") {
		return s
	}
	drop := make([]bool, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '@' {
			continue
		}
		for j := i - 1; j >= 0 && strings.IndexByte(atBreaks, s[j]) >= 0; j-- {
			drop[j] = true
		}
		for j := i + 1; j < len(s) && strings.IndexByte(atBreaks, s[j]) >= 0; j++ {
			drop[j] = true
		}
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if !drop[i] {
			b = append(b, s[i])
		}
	}
	return string(b)
}

// idDigits is s, its digits folded to ASCII, with each run of digits glued
// after a letter masked (INC0012345 reads as INCxxxxxxx): an id's digits,
// not a phone number's.
func idDigits(s string) string {
	s = foldDigits(s)
	b := []byte(s)
	for i := 0; i < len(b); {
		if !isDigit(b[i]) {
			i++
			continue
		}
		j := i
		for j < len(b) && isDigit(b[j]) {
			j++
		}
		if r, _ := utf8.DecodeLastRuneInString(s[:i]); unicode.IsLetter(r) {
			for k := i; k < j; k++ {
				b[k] = 'x'
			}
		}
		i = j
	}
	return string(b)
}

// unescapePercent is s with its %XX escapes decoded, any other % left as
// it is.
func unescapePercent(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(n))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// strayAddress is the net under the key rules: it reports an address or a
// phone number (netAddresses) in any string of the config but the op's
// text (its text and subject params, and proseKeys), its sender
// (SenderKey) and the one recipient in to. Whatever key a node reads a
// copy from, a rule never stands for a send that names anyone else. Only
// a string is prose: an object under a prose key ({"message": {"to": …}},
// Graph's message) is walked like any other. Under a time-like key
// (timeKey: send_at, createdAt), 10 or 13 digits that read as a Unix time
// are one (epoch), not a phone number.
func strayAddress(t target, nodeType string, config map[string]interface{}, to map[string]bool) bool {
	own := map[string]bool{}
	for v := range to {
		netAddresses(v, func(a string) bool {
			own[a] = true
			return true
		})
	}
	prose := map[string]bool{}
	for _, k := range append(append(append([]string{}, t.text...), t.subject...), proseKeys...) {
		prose[joined(k)] = true
		if _, leaf, dotted := strings.Cut(k, "."); dotted {
			prose[joined(leaf)] = true
		}
	}
	// v is under key; inProse: under a prose key, as itself or in a list.
	var walk func(v interface{}, key string, inProse bool, depth int) bool
	walk = func(v interface{}, key string, inProse bool, depth int) bool {
		if depth > 8 {
			return true // too deep to judge: the card decides
		}
		switch x := v.(type) {
		case string:
			return !inProse && !netAddresses(x, func(a string) bool { return own[a] || timeKey(key) && !recipientLike(key) && epoch(a) })
		case []string:
			for _, s := range x {
				if walk(s, key, inProse, depth+1) {
					return true
				}
			}
		case []interface{}:
			for _, s := range x {
				if walk(s, key, inProse, depth+1) {
					return true
				}
			}
		case map[string]interface{}:
			for k, sub := range x {
				if senderWord(k) || (depth == 0 && SenderKey(nodeType, k)) {
					continue
				}
				if walk(sub, k, prose[joined(k)], depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(config, "", false, 0)
}
