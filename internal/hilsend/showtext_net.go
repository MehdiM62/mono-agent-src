package hilsend

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// shownParam is a param the card shows, with its value as parsed.
type shownParam struct {
	path  string
	value interface{}
}

// net is the card's guarantee past its word lists: it lists every
// address, link, @handle, phone number, channel and long id in what the
// step sends (each param's string leaves, its key names, and the numbers
// under a key with "id" in it) that the card's text doesn't already show,
// each with its path, under the fields. Whatever the caps left out, and
// however its keys are named, none of those is out of sight: when they
// don't all fit, the card is cut and can't be approved from.
func (c *card) net(params []shownParam) {
	visible := map[string]bool{}
	for _, t := range addressTokens(c.b.String()) {
		visible[t] = true
	}
	listed := map[string]bool{}
	header := false
	add := func(path, token string) {
		if c.cut || visible[token] || listed[token] {
			return
		}
		listed[token] = true
		if !header {
			header = true
			c.put("\nAlso in what it sends, not shown above (addresses, links, handles, phone numbers, ids):", true)
		}
		c.put("\n  "+cardValue(path, showValueMax)+": "+cardValue(token, showValueMax), true)
	}
	var walk func(v interface{}, path, key string)
	walk = func(v interface{}, path, key string) {
		if c.cut {
			return
		}
		switch x := v.(type) {
		case map[string]interface{}:
			for _, k := range sortedKeys(x) {
				for _, t := range addressTokens(k) {
					add(path, t)
				}
				walk(x[k], joinPath(path, k), k)
			}
		case []interface{}:
			for i, el := range x {
				walk(el, path+"["+strconv.Itoa(i)+"]", key)
			}
		case string:
			for _, t := range addressTokens(x) {
				add(path, t)
			}
			if idKey(key) && longNumber(x) {
				add(path, x)
			}
		case float64:
			if n := strconv.FormatFloat(x, 'f', -1, 64); idKey(key) && longNumber(n) {
				add(path, n)
			}
		}
	}
	for _, p := range params {
		walk(p.value, p.path, p.path[strings.LastIndex(p.path, ".")+1:])
	}
}

// addressTokens finds what in s addresses someone: email addresses
// (a@b.c), links (any scheme with a host), @handles, #channels, Slack-style
// channel ids (C0…), and phone numbers (+ and 7 to 15 digits, as "+"
// and the digits).
func addressTokens(s string) []string {
	var out []string
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '+' || s[i+1] < '0' || s[i+1] > '9' {
			continue
		}
		digits := []byte{'+'}
		j := i + 1
	number:
		for ; j < len(s) && len(digits) <= 16; j++ {
			switch b := s[j]; {
			case b >= '0' && b <= '9':
				digits = append(digits, b)
			case b == ' ' || b == '-' || b == '.' || b == '(' || b == ')':
			default:
				break number
			}
		}
		if n := len(digits) - 1; n >= 7 && n <= 15 {
			out = append(out, string(digits))
		}
		i = j - 1
	}
	for _, t := range strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",;\"'<>()[]{}|\\`", r)
	}) {
		if t = strings.TrimRight(t, ".:!?"); t == "" {
			continue
		}
		switch at := strings.LastIndexByte(t, '@'); {
		case at > 0 && at < len(t)-1 && strings.Contains(t[at+1:], "."):
			out = append(out, t)
		case strings.Contains(t, "://") && !strings.HasSuffix(t, "://") && !strings.HasPrefix(t, "://"):
			out = append(out, t)
		case (t[0] == '@' || t[0] == '#') && len(t) >= 3 && handle(t[1:]):
			out = append(out, t)
		case channelID(t):
			out = append(out, t)
		}
	}
	return out
}

// handle reports a handle's or a channel name's characters, a letter
// among them.
func handle(s string) bool {
	letter := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r) || r == '_' || r == '.' || r == '-':
		default:
			return false
		}
	}
	return letter
}

// channelID reports a Slack-style channel, group or user id: C, G, D, U
// or W and 8 to 11 upper-case letters and digits, a digit among them.
func channelID(s string) bool {
	if len(s) < 9 || len(s) > 12 || !strings.ContainsRune("CGDUW", rune(s[0])) {
		return false
	}
	digit := false
	for _, r := range s[1:] {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'A' && r <= 'Z':
		default:
			return false
		}
	}
	return digit
}

// idKey reports a key with "id" among its words (chat_id, principalId).
func idKey(k string) bool {
	if len(k) > showKeyNameMax {
		return false
	}
	for _, w := range keyWords(k) {
		if w == "id" || w == "ids" {
			return true
		}
	}
	return false
}

// longNumber reports a number of 6 digits or more (a minus sign allowed).
func longNumber(s string) bool {
	s = strings.TrimPrefix(s, "-")
	if len(s) < 6 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// leftOut says what a recipient-like list leaves out: how many elements,
// and the distinct domains of the addresses and links among them.
func leftOut(hidden []interface{}) string {
	var words []string
	for _, el := range hidden {
		raw, _ := json.Marshal(el)
		words = append(words, string(raw))
	}
	return fmt.Sprintf("%d more elements%s …", len(hidden), domainsIn(strings.Join(words, " ")))
}

// domainSep separates the words domainsIn reads.
func domainSep(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`,;"'<>()[]{}\`, r) }

// domainsIn names every distinct domain of the addresses (a@domain) and
// links (scheme://host) in s: none is left out (the card is cut, and can't
// be approved from, when they don't fit).
func domainsIn(s string) string {
	seen := map[string]bool{}
	var domains []string
	for _, tok := range strings.FieldsFunc(s, domainSep) {
		host := ""
		if at := strings.LastIndexByte(tok, '@'); at >= 0 {
			host = tok[at+1:]
		} else if _, rest, ok := strings.Cut(tok, "://"); ok {
			host, _, _ = strings.Cut(rest, "/")
		}
		host = strings.ToLower(strings.TrimRight(host, ".:"))
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		domains = append(domains, host)
	}
	if len(domains) == 0 {
		return ""
	}
	sort.Strings(domains)
	return fmt.Sprintf(", %d distinct domains: %s", len(domains), escapeHidden(strings.Join(domains, ", ")))
}
