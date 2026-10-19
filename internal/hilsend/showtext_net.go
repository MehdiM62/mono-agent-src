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
// address, link, @handle, phone number, channel and id in what the step
// sends (each param's string leaves, its key names, and the numbers under
// a key with "id" in it) that the card's text doesn't already show, each
// with its path, under the fields. One in a recipient-like field (to, a
// pair's value) is listed unless a recipient-like field shows it: one that
// shows only in a field such as notes still reads as no recipient.
// Whatever the caps left out, and however its keys are named, none of
// those is out of sight: when they don't all fit, even after the card was
// cut elsewhere, the card can't be approved from.
func (c *card) net(params []shownParam) {
	if c.cut && c.unreviewable != "" {
		return
	}
	inFields, elsewhere := map[string]bool{}, map[string]bool{}
	text, at := c.b.String(), 0
	see := func(s string, seen map[string]bool) {
		for _, t := range addressTokens(cardText.Replace(s)) {
			seen[t] = true
		}
	}
	for _, f := range c.fields {
		see(text[at:f[0]], elsewhere)
		see(text[f[0]:f[1]], inFields)
		at = f[1]
	}
	see(text[at:], elsewhere)
	type hit struct{ path, token string }
	var first, rest []hit
	// The walk stops once its lines can't fit (each takes at least its
	// token and 9 bytes): the card is cut, and can't be approved from,
	// whatever else it would find.
	room, counted := showCardMax-c.b.Len(), map[string]bool{}
	found := func(path, token string, field bool) {
		switch {
		case inFields[token]:
			return
		case field:
			first = append(first, hit{path, token})
		case !elsewhere[token]:
			rest = append(rest, hit{path, token})
		default:
			return
		}
		if !counted[token] {
			counted[token] = true
			room -= min(len(token), showValueMax/2) + 9
		}
	}
	var walk func(v interface{}, path, key string, field bool)
	walk = func(v interface{}, path, key string, field bool) {
		if room < 0 {
			return
		}
		switch x := v.(type) {
		case map[string]interface{}:
			by, _ := c.pair(x)
			for _, k := range sortedKeys(x) {
				in := field || c.recipient(k) || by != "" && k != by
				for _, t := range addressTokens(k) {
					found(path, t, in)
				}
				walk(x[k], joinPath(path, k), k, in)
			}
		case []interface{}:
			for i, el := range x {
				walk(el, path+"["+strconv.Itoa(i)+"]", key, field)
			}
		case string:
			for _, t := range addressTokens(x) {
				if t != x || !timeKey(key) || len(x) != 10 && len(x) != 13 {
					found(path, t, field)
				}
			}
			if idKey(key) && longNumber(x) {
				found(path, x, field)
			}
		case float64:
			if n := strconv.FormatFloat(x, 'f', -1, 64); idKey(key) && longNumber(n) {
				found(path, n, field)
			}
		}
	}
	for _, p := range params {
		key := p.path[strings.LastIndex(p.path, ".")+1:]
		walk(p.value, p.path, key, c.recipient(key))
	}
	listed := map[string]bool{}
	for _, h := range append(first, rest...) {
		if listed[h.token] {
			continue
		}
		if len(listed) == 0 {
			c.put("\nAlso in what it sends, not shown above (addresses, links, handles, phone numbers, ids):", true)
		}
		listed[h.token] = true
		c.put("\n  "+cardValue(h.path, showValueMax)+": "+cardValue(h.token, showValueMax), true)
		if c.cut {
			return
		}
	}
}

// cardText is how the net reads the card's text: its JSON values' escapes
// of <, > and & as those characters, and \n, \r, \t and \\ as the word
// breaks they are in what the step sends.
var cardText = strings.NewReplacer(`\\`, " ", `\n`, " ", `\r`, " ", `\t`, " ", `\u003c`, "<", `\u003e`, ">", `\u0026`, "&")

// timeWords are the words of a key whose value is a time: under one, 10
// or 13 digits alone are a Unix time in seconds or milliseconds, not a
// phone number.
var timeWords = map[string]bool{"time": true, "times": true, "timestamp": true, "timestamps": true, "ts": true,
	"date": true, "datetime": true, "at": true, "on": true, "created": true, "updated": true, "modified": true,
	"expires": true, "expiry": true, "since": true, "until": true, "epoch": true, "unix": true, "start": true,
	"end": true, "due": true, "deadline": true, "when": true}

// timeKey reports a key with a time word among its words (createdAt, ts).
func timeKey(k string) bool {
	if len(k) > showKeyNameMax {
		return false
	}
	for _, w := range keyWords(k) {
		if timeWords[w] {
			return true
		}
	}
	return false
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
