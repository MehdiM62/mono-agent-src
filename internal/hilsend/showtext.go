package hilsend

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What the card shows of one value: a value is cut, never what follows it,
// so padding can't push a key or a change out of sight.
const (
	showValueMax = 500  // bytes of a param's value
	showKeyMax   = 200  // bytes of one key's value in an object
	showKeysMax  = 20   // keys of an object listed with their values
	showElemsMax = 10   // elements of a list listed one by one
	showNameMax  = 80   // bytes of one key's name
	showNamesMax = 2000 // bytes of a list of key names
)

// showText is the card's text for a send whose words aren't a message the
// approver could edit: the op, and each param it shows with its value as
// this item would send it, so the user sees what they approve: the keys it
// doesn't declare (undeclared), or the params its annotation shows, those
// the config sets. An object, or raw JSON text that holds one, lists each
// top-level key with its own value, every key's name in sight; a list
// lists each element, an object element its keys. A param
// given as an expression (exprs) says so: its keys are the run's, and on an
// empty item it resolves to "", which would read as nothing undeclared. One
// the run doesn't send (unsent: raw JSON its switch leaves off) is marked
// "(not sent)".
func showText(op string, show, exprs, unsent []string, undeclared bool, config map[string]interface{}) string {
	var b strings.Builder
	if undeclared {
		b.WriteString(op + " with fields it doesn't declare:")
	} else {
		b.WriteString(op + " with:")
	}
	for _, path := range show {
		label := path
		if slices.Contains(unsent, path) {
			label += " (not sent)"
		}
		if slices.Contains(exprs, path) {
			b.WriteString("\n" + label + ": (an expression)")
			continue
		}
		val := valueAt(config, path)
		if val == nil && !undeclared {
			continue
		}
		switch x := structured(val).(type) {
		case map[string]interface{}:
			b.WriteString("\n" + label + ":")
			writeKeys(&b, x, "  ")
		case []interface{}:
			b.WriteString("\n" + label + ":")
			writeElements(&b, x, "  ")
		default:
			b.WriteString("\n" + label + ": " + cardValue(val, showValueMax))
		}
	}
	return b.String()
}

// structured is v as an object or a list, a collection's or raw JSON text
// holding one, or v itself.
func structured(v interface{}) interface{} {
	if x, ok := v.(string); ok {
		var parsed interface{}
		if t := strings.TrimSpace(x); (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Unmarshal([]byte(t), &parsed) == nil {
			return parsed
		}
	}
	return v
}

// writeElements lists arr's elements one by one, an object's keys under
// it. Past showElemsMax it lists only an object element with a
// recipient-like key, up to showElemsMax more, and names the rest of
// those; then says how many more elements there are.
func writeElements(b *strings.Builder, arr []interface{}, indent string) {
	if len(arr) == 0 {
		b.WriteString(" []")
		return
	}
	extra, more := 0, 0
	var named []string
	for i, el := range arr {
		obj, isObj := el.(map[string]interface{})
		if i >= showElemsMax {
			rk := firstRecipientLike(obj)
			switch {
			case rk == "":
				more++
				continue
			case extra == showElemsMax:
				named = append(named, fmt.Sprintf("[%d] has %s", i, cardValue(rk, showNameMax)))
				continue
			}
			extra++
		}
		if isObj {
			fmt.Fprintf(b, "\n%s[%d]:", indent, i)
			writeKeys(b, obj, indent+"  ")
			continue
		}
		fmt.Fprintf(b, "\n%s[%d]: %s", indent, i, cardValue(el, showKeyMax))
	}
	if more > 0 {
		fmt.Fprintf(b, "\n%s+%d more elements", indent, more)
	}
	if len(named) > 0 {
		fmt.Fprintf(b, "\n%salso with recipient keys: %s", indent, nameList(named, false))
	}
}

// writeKeys lists obj's keys, each on its own line with its value: the
// recipient-like ones first, all of them, then showKeysMax others, and
// past those the names of the rest. A value that is an
// object itself names all its keys under it, recipient-like first.
func writeKeys(b *strings.Builder, obj map[string]interface{}, indent string) {
	if len(obj) == 0 {
		b.WriteString(" {}")
		return
	}
	keys := orderKeys(obj)
	others := 0
	for i, k := range keys {
		recipient := recipientLike(k)
		if !recipient {
			if others == showKeysMax {
				fmt.Fprintf(b, "\n%s+%d more keys: %s", indent, len(keys)-i, nameList(keys[i:], true))
				return
			}
			others++
		}
		name := cardValue(k, showNameMax)
		if recipient {
			name = cardValue(k, showValueMax)
		}
		b.WriteString("\n" + indent + name + ": " + cardValue(obj[k], showKeyMax))
		if inner, ok := obj[k].(map[string]interface{}); ok && len(inner) > 0 {
			b.WriteString("\n" + indent + "  keys: " + nameList(orderKeys(inner), true))
		}
	}
}

// orderKeys is obj's keys, sorted, the recipient-like ones first.
func orderKeys(obj map[string]interface{}) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if ri, rj := recipientLike(keys[i]), recipientLike(keys[j]); ri != rj {
			return ri
		}
		return keys[i] < keys[j]
	})
	return keys
}

// nameList joins names (JSON-quoted when quote) up to showNamesMax bytes,
// saying how many more there are past that.
func nameList(names []string, quote bool) string {
	var b strings.Builder
	for i, n := range names {
		if quote {
			n = cardValue(n, showNameMax)
		}
		if i > 0 && b.Len()+len(n)+2 > showNamesMax {
			fmt.Fprintf(&b, ", +%d more names", len(names)-i)
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(n)
	}
	return b.String()
}

// recipientWords are the words of a key that may say whom a send reaches
// (internal/apipkg's send lint takes the same ones for a recipient param).
var recipientWords = map[string]bool{"to": true, "cc": true, "bcc": true, "email": true, "emails": true, "mail": true,
	"mailto": true, "recipient": true, "recipients": true, "phone": true, "mobile": true, "tel": true, "telephone": true,
	"sms": true, "msisdn": true, "whatsapp": true, "channel": true, "channels": true, "chat": true, "user": true,
	"users": true, "url": true, "uri": true, "webhook": true, "endpoint": true, "address": true, "addresses": true,
	"host": true, "receiver": true, "receivers": true, "addressee": true, "addressees": true, "attendee": true,
	"attendees": true, "invitee": true, "invitees": true, "guest": true, "guests": true, "participant": true,
	"participants": true, "subscriber": true, "subscribers": true, "invite": true, "invites": true, "invitation": true,
	"notify": true, "notification": true, "notifications": true, "welcome": true, "activate": true, "activation": true}

var (
	keyHump  = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	keySplit = regexp.MustCompile(`[^A-Za-z0-9]+`)
)

// recipientLike reports a key one of whose words (camelCase, snake_case or
// kebab-case) may say whom a send reaches: zz_to, recipientEmail, chat_id.
func recipientLike(k string) bool {
	for _, w := range keySplit.Split(keyHump.ReplaceAllString(k, "$1 $2"), -1) {
		if recipientWords[strings.ToLower(w)] {
			return true
		}
	}
	return false
}

// firstRecipientLike is obj's first recipient-like key, or "".
func firstRecipientLike(obj map[string]interface{}) string {
	for _, k := range orderKeys(obj) {
		if recipientLike(k) {
			return k
		}
		return ""
	}
	return ""
}

// cardValue is v as JSON, cut to max bytes, with the characters that read
// as nothing or change how text reads escaped.
func cardValue(v interface{}, max int) string {
	raw, _ := json.Marshal(v)
	return escapeHidden(cutBytes(string(raw), max))
}

// cutBytes cuts s to at most max bytes on a rune boundary, ending in "…"
// when it cut.
func cutBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	i := max
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i] + "…"
}

// escapeHidden writes as \uXXXX (\u{XXXXX} past U+FFFF) each character a
// reader doesn't see or that changes how text reads: format characters
// (U+202E right-to-left override, U+200B, the tag block), controls, line
// and paragraph separators, every space but the ASCII one, variation
// selectors, private-use and unassigned code points.
func escapeHidden(s string) string {
	var b strings.Builder
	for _, r := range s {
		if !hiddenRune(r) {
			b.WriteRune(r)
			continue
		}
		if r > 0xFFFF {
			fmt.Fprintf(&b, `\u{%X}`, r)
		} else {
			fmt.Fprintf(&b, `\u%04X`, r)
		}
	}
	return b.String()
}

func hiddenRune(r rune) bool {
	switch {
	case unicode.IsControl(r), unicode.In(r, unicode.Cf, unicode.Co, unicode.Zl, unicode.Zp, unicode.Variation_Selector):
		return true
	case unicode.Is(unicode.Zs, r):
		return r != ' '
	}
	return !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z) // unassigned
}
