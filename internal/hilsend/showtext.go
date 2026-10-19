package hilsend

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What the card shows of one value: a value is cut, never what follows it,
// so padding can't push a key or a change out of sight.
const (
	showValueMax        = 500   // bytes of a param's value
	showKeyMax          = 200   // bytes of one key's value in an object
	showKeysMax         = 20    // keys of an object listed with their values, besides the recipient-like ones
	showElemsMax        = 10    // elements of a list listed one by one
	showNameMax         = 80    // bytes of one key's name
	showNamesMax        = 2000  // bytes of a list of key names
	showRecipientMax    = 4096  // bytes of a recipient-like key's value
	showRecipientBudget = 65536 // bytes of recipient-like values on one card
	showDepthMax        = 4     // levels of nested names under a value
	showNestedMax       = 2000  // nested names visited under one value
	showClassifyMax     = 5000  // keys checked for recipients on one card
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
	c := &card{recipients: map[string]bool{}, fullLeft: showRecipientBudget}
	if undeclared {
		c.b.WriteString(op + " with fields it doesn't declare:")
	} else {
		c.b.WriteString(op + " with:")
	}
	for _, path := range show {
		label := path
		if slices.Contains(unsent, path) {
			label += " (not sent)"
		}
		if slices.Contains(exprs, path) {
			c.b.WriteString("\n" + label + ": (an expression)")
			continue
		}
		val := valueAt(config, path)
		if val == nil && !undeclared {
			continue
		}
		switch x := structured(val).(type) {
		case map[string]interface{}:
			c.b.WriteString("\n" + label + ":")
			c.keys(x, "  ")
		case []interface{}:
			if c.recipient(path[strings.LastIndex(path, ".")+1:]) {
				c.fullValue(label, x, "")
				continue
			}
			c.b.WriteString("\n" + label + ":")
			c.elements(x, "  ")
		default:
			c.b.WriteString("\n" + label + ": " + cardValue(val, showValueMax))
		}
	}
	if c.unchecked {
		fmt.Fprintf(&c.b, "\n(more than %d key names: the rest weren't checked for recipient fields)", showClassifyMax)
	}
	return c.b.String()
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

// card writes a send card's text. Each key is checked for a recipient
// once, and no more than showClassifyMax of them (unchecked says some
// weren't); recipient-like values share fullLeft bytes.
type card struct {
	b          strings.Builder
	recipients map[string]bool
	unchecked  bool
	fullLeft   int
}

// recipient reports whether key k may name whom a send reaches.
func (c *card) recipient(k string) bool {
	if v, ok := c.recipients[k]; ok {
		return v
	}
	if len(c.recipients) >= showClassifyMax {
		c.unchecked = true
		return false
	}
	v := recipientLike(k)
	c.recipients[k] = v
	return v
}

// pairName is the field obj names when it is a name and value pair
// ({"name": "recipientEmail", "value": …}, and with key or field for
// name) whose name is recipient-like: its value is that field's.
func (c *card) pairName(obj map[string]interface{}) string {
	if _, ok := obj["value"]; !ok {
		return ""
	}
	for _, n := range []string{"name", "key", "field"} {
		if s, ok := obj[n].(string); ok && c.recipient(s) {
			return s
		}
	}
	return ""
}

// order is obj's keys, sorted, the recipient-like ones first (a pair's
// value among them), with which are.
func (c *card) order(obj map[string]interface{}) ([]string, []bool) {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pair := c.pairName(obj) != ""
	var first, rest []string
	for _, k := range keys {
		if c.recipient(k) || (pair && k == "value") {
			first = append(first, k)
		} else {
			rest = append(rest, k)
		}
	}
	flags := make([]bool, len(keys))
	for i := range first {
		flags[i] = true
	}
	return append(first, rest...), flags
}

// firstRecipient is obj's first recipient-like key, or the field a name
// and value pair names, or "".
func (c *card) firstRecipient(obj map[string]interface{}) string {
	if p := c.pairName(obj); p != "" {
		return p
	}
	first := ""
	for k := range obj {
		if (first == "" || k < first) && c.recipient(k) {
			first = k
		}
	}
	return first
}

// elements lists arr's elements one by one, an object's keys under it, a
// nested one's names. Past showElemsMax it lists only an object element
// with a recipient-like key, up to showElemsMax more, and names the rest
// of those; then says how many more elements there are.
func (c *card) elements(arr []interface{}, indent string) {
	if len(arr) == 0 {
		c.b.WriteString(" []")
		return
	}
	extra, more := 0, 0
	var named []string
	for i, el := range arr {
		obj, isObj := el.(map[string]interface{})
		if i >= showElemsMax {
			rk := ""
			if isObj {
				rk = c.firstRecipient(obj)
			}
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
			fmt.Fprintf(&c.b, "\n%s[%d]:", indent, i)
			c.keys(obj, indent+"  ")
			continue
		}
		fmt.Fprintf(&c.b, "\n%s[%d]: %s", indent, i, cardValue(el, showKeyMax))
		c.nestedLine(el, indent+"  ")
	}
	if more > 0 {
		fmt.Fprintf(&c.b, "\n%s+%d more elements", indent, more)
	}
	if len(named) > 0 {
		fmt.Fprintf(&c.b, "\n%salso with recipient keys: %s", indent, nameList(named, false))
	}
}

// keys lists obj's keys, each on its own line with its value: the
// recipient-like ones first, all of them, with their values in full (up to
// showRecipientMax), then showKeysMax others, and past those the names of
// the rest. A value that is an object or a list names the keys in it.
func (c *card) keys(obj map[string]interface{}, indent string) {
	if len(obj) == 0 {
		c.b.WriteString(" {}")
		return
	}
	keys, recipient := c.order(obj)
	pair := c.pairName(obj)
	others := 0
	for i, k := range keys {
		if !recipient[i] {
			if others == showKeysMax {
				fmt.Fprintf(&c.b, "\n%s+%d more keys: %s", indent, len(keys)-i, nameList(keys[i:], true))
				return
			}
			others++
			c.b.WriteString("\n" + indent + cardValue(k, showNameMax) + ": " + cardValue(obj[k], showKeyMax))
		} else {
			name := cardValue(k, showValueMax)
			if pair != "" && k == "value" {
				name += " (" + cardValue(pair, showNameMax) + ")"
			}
			c.fullValue(name, obj[k], indent)
		}
		c.nestedLine(obj[k], indent+"  ")
	}
}

// fullValue writes a recipient-like key's value in full, up to
// showRecipientMax bytes and what is left of the card's budget: a list
// one element a line, then how many more.
func (c *card) fullValue(name string, v interface{}, indent string) {
	limit := max(min(showRecipientMax, c.fullLeft), showKeyMax)
	arr, isList := v.([]interface{})
	if !isList || len(arr) == 0 {
		s := cardValue(v, limit)
		c.fullLeft -= len(s)
		c.b.WriteString("\n" + indent + name + ": " + s)
		return
	}
	c.b.WriteString("\n" + indent + name + ":")
	used := 0
	for i, el := range arr {
		s := cardValue(el, showKeyMax)
		if i > 0 && used+len(s) > limit {
			fmt.Fprintf(&c.b, "\n%s  +%d more elements", indent, len(arr)-i)
			break
		}
		used += len(s)
		fmt.Fprintf(&c.b, "\n%s  [%d]: %s", indent, i, s)
	}
	c.fullLeft -= used
}

// nestedLine names the keys inside v, an object or a list, under its line:
// every level down to showDepthMax, as paths ("b.to", "[1].to"), the
// recipient-like ones first, up to showNamesMax bytes.
func (c *card) nestedLine(v interface{}, indent string) {
	var recipients, others []string
	visited := 0
	var walk func(v interface{}, prefix string, depth int)
	walk = func(v interface{}, prefix string, depth int) {
		if depth > showDepthMax {
			return
		}
		switch x := v.(type) {
		case map[string]interface{}:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if visited++; visited > showNestedMax {
					return
				}
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				if c.recipient(k) {
					recipients = append(recipients, p)
				} else {
					others = append(others, p)
				}
				walk(x[k], p, depth+1)
			}
		case []interface{}:
			for i, el := range x {
				if visited++; visited > showNestedMax {
					return
				}
				walk(el, fmt.Sprintf("%s[%d]", prefix, i), depth+1)
			}
		}
	}
	walk(v, "", 1)
	if names := append(recipients, others...); len(names) > 0 {
		c.b.WriteString("\n" + indent + "keys: " + nameList(names, true))
	}
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
