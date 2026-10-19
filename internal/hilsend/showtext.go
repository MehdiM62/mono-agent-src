package hilsend

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// What the card shows of one value: a value is cut, never what follows it,
// so padding can't push a key or a change out of sight. A field that may
// name a recipient is never cut to a few bytes: its value shows in full up
// to its share, and past that its head, its last element and the domains
// in what is left out. A card that can't show all of that, or whose value
// is too large to check, can't be approved from (unreviewable).
const (
	showValueMax      = 500       // bytes of a param's value
	showKeyMax        = 200       // bytes of one key's value in an object
	showKeysMax       = 20        // keys of an object listed with their values, besides the recipient-like ones
	showElemsMax      = 10        // elements of a list listed one by one, besides those holding a recipient
	showNameMax       = 80        // bytes of one key's name
	showNamesMax      = 2000      // bytes of a list of key names
	showRecipientMax  = 4096      // bytes of a recipient-like value, with few of them in one object
	showRecipientMin  = 512       // bytes of each, past showRecipientsFew of them
	showRecipientsFew = 8         // recipient-like values in one object that get showRecipientMax each
	showListHead      = 5         // elements of a recipient-like list always shown, with its last one
	showDepthMax      = 4         // levels of names listed under a value (recipient-like ones at any depth)
	showParamKeysMax  = 1_000_000 // keys and elements of one param a card checks
	showRawMax        = 8 << 20   // bytes of raw JSON text a card parses
	showKeyNameMax    = 256       // bytes of a key name a card checks
	showCardMax       = 256 << 10 // bytes of a card
)

// showText is the card's text (showCard) alone.
func showText(op string, show, exprs, unsent []string, undeclared bool, config map[string]interface{}) string {
	text, _ := showCard(op, show, exprs, unsent, undeclared, config)
	return text
}

// showCard is the card's text for a send whose words aren't a message the
// approver could edit: the op, and each param it shows with its value as
// this item would send it, so the user sees what they approve: the keys it
// doesn't declare (undeclared), or the params its annotation shows, those
// the config sets. An object, or raw JSON text that holds one, lists each
// top-level key with its own value, recipient-like ones first, every key's
// name in sight; a list lists each element, an object element its keys. A
// param given as an expression (exprs) says so: its keys are the run's, and
// on an empty item it resolves to "", which would read as nothing
// undeclared. One the run doesn't send (unsent: raw JSON its switch leaves
// off) is marked "(not sent)". After the fields, the card lists every
// address, link, handle, phone number and id in what the step sends that
// it doesn't already show (net). unreviewable says why the card can't be
// approved from, "" when it can: a param too large to check, or a card cut
// where it shows a recipient or before the net has listed all it found.
func showCard(op string, show, exprs, unsent []string, undeclared bool, config map[string]interface{}) (text, unreviewable string) {
	c := &card{recipients: map[string]bool{}}
	var shown []shownParam
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
			c.put("\n"+label+": (an expression)", false)
			continue
		}
		val := valueAt(config, path)
		if val == nil && !undeclared {
			continue
		}
		v, raw := structured(val)
		if raw > showRawMax {
			c.put(fmt.Sprintf("\n%s: too large to review: %d bytes of JSON text", label, raw), true)
			c.refuse(fmt.Sprintf("%s holds %d bytes of JSON text, over %d MB: too large to review", path, raw, showRawMax>>20))
			continue
		}
		if n := countNodes(v, showParamKeysMax); n > showParamKeysMax {
			c.put(fmt.Sprintf("\n%s: too large to review: more than %d keys and elements", label, showParamKeysMax), true)
			c.refuse(fmt.Sprintf("%s holds more than %d keys and elements: too large to review", path, showParamKeysMax))
			continue
		}
		shown = append(shown, shownParam{path, v})
		if c.recipient(path[strings.LastIndex(path, ".")+1:]) {
			c.fullValue(label, v, "", showRecipientMax)
			continue
		}
		switch x := v.(type) {
		case map[string]interface{}:
			c.put("\n"+label+":", false)
			c.keys(x, "  ")
		case []interface{}:
			c.put("\n"+label+":", false)
			c.elements(x, "  ")
		default:
			c.put("\n"+label+": "+cardValue(val, showValueMax), false)
		}
	}
	c.net(shown)
	if c.cut {
		c.b.WriteString("\n(card cut)")
	}
	return c.b.String(), c.unreviewable
}

// structured is v as an object or a list, a collection's or raw JSON text
// holding one, or v itself, and the bytes of the raw JSON text: past
// showRawMax it isn't parsed.
func structured(v interface{}) (interface{}, int) {
	if x, ok := v.(string); ok {
		if t := strings.TrimSpace(x); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
			if len(t) > showRawMax {
				return v, len(t)
			}
			var parsed interface{}
			if json.Unmarshal([]byte(t), &parsed) == nil {
				return parsed, len(t)
			}
		}
	}
	return v, 0
}

// countNodes counts v's keys and elements, at every depth, up to past max.
func countNodes(v interface{}, max int) int {
	n := 0
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case map[string]interface{}:
			for _, e := range x {
				if n++; n > max {
					return
				}
				walk(e)
			}
		case []interface{}:
			for _, e := range x {
				if n++; n > max {
					return
				}
				walk(e)
			}
		}
	}
	walk(v)
	return n
}

// card writes a send card's text, up to showCardMax bytes; each key is
// checked for a recipient once.
type card struct {
	b            strings.Builder
	recipients   map[string]bool
	cut          bool     // something didn't fit showCardMax
	unreviewable string   // why the card can't be approved from
	inField      bool     // writing a recipient-like field's value (fullValue)
	fields       [][2]int // where the text shows the outermost of those: start, end
}

// put writes s, or, past showCardMax, nothing more: when s shows a
// recipient, the card can't be approved from.
func (c *card) put(s string, recipient bool) {
	if c.cut || c.b.Len()+len(s) > showCardMax {
		c.cut = true
		if recipient && c.unreviewable == "" {
			c.unreviewable = fmt.Sprintf("the card is over %d KB and is cut where it shows a field that may name a recipient", showCardMax>>10)
		}
		return
	}
	c.b.WriteString(s)
}

// refuse records why the card can't be approved from (the first reason).
func (c *card) refuse(why string) {
	if c.unreviewable == "" {
		c.unreviewable = why
	}
}

// recipient reports whether key k may name whom a send reaches. A key over
// showKeyNameMax bytes is taken for one, unchecked, and the card can't be
// approved from.
func (c *card) recipient(k string) bool {
	if len(k) > showKeyNameMax {
		c.refuse(fmt.Sprintf("a key of %d bytes, over %d: too large to review", len(k), showKeyNameMax))
		return true
	}
	v, ok := c.recipients[k]
	if !ok {
		v = recipientLike(k)
		c.recipients[k] = v
	}
	return v
}

// pair is obj as a name and value pair: a small object one of whose keys
// holds a recipient-like name ({"name": "recipientEmail", "value": …},
// {"fieldName": "to", "fieldValue": …}, {"label": "To", "value": …},
// {"type": "recipient", "value": …}); by is that key and name the name,
// and its other keys hold that field's value. by is "" when obj is none.
func (c *card) pair(obj map[string]interface{}) (by, name string) {
	if len(obj) < 2 || len(obj) > 8 {
		return "", ""
	}
	for _, k := range sortedKeys(obj) {
		if s, ok := obj[k].(string); ok && len(s) <= 64 && c.recipient(s) {
			return k, s
		}
	}
	return "", ""
}

// order is obj's keys, sorted, the recipient-like ones first (a pair's
// values among them), and how many of those there are.
func (c *card) order(obj map[string]interface{}) ([]string, int) {
	keys := sortedKeys(obj)
	by, _ := c.pair(obj)
	var first, rest []string
	for _, k := range keys {
		if c.recipient(k) || (by != "" && k != by) {
			first = append(first, k)
		} else {
			rest = append(rest, k)
		}
	}
	return append(first, rest...), len(first)
}

func sortedKeys(obj map[string]interface{}) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// holdsRecipient reports a recipient-like key, or a name and value pair
// naming one, anywhere in v.
func (c *card) holdsRecipient(v interface{}) bool {
	switch x := v.(type) {
	case map[string]interface{}:
		if by, _ := c.pair(x); by != "" {
			return true
		}
		for k, e := range x {
			if c.recipient(k) || c.holdsRecipient(e) {
				return true
			}
		}
	case []interface{}:
		for _, e := range x {
			if c.holdsRecipient(e) {
				return true
			}
		}
	}
	return false
}

// elements lists arr's elements one by one, an object's keys under it, a
// nested one's names. Past showElemsMax it lists only the elements that
// hold a recipient, all of them, and says how many more there are.
func (c *card) elements(arr []interface{}, indent string) {
	if len(arr) == 0 {
		c.put(" []", false)
		return
	}
	more := 0
	for i, el := range arr {
		recipient := i >= showElemsMax
		if recipient && !c.holdsRecipient(el) {
			more++
			continue
		}
		c.element(i, el, indent, recipient)
	}
	if more > 0 {
		c.put(fmt.Sprintf("\n%s+%d more elements", indent, more), false)
	}
}

// element writes element i of a list: an object's keys under it, another
// value with the names in it.
func (c *card) element(i int, el interface{}, indent string, recipient bool) {
	if obj, ok := el.(map[string]interface{}); ok {
		c.put(fmt.Sprintf("\n%s[%d]:", indent, i), recipient)
		c.keys(obj, indent+"  ")
		return
	}
	c.put(fmt.Sprintf("\n%s[%d]: %s", indent, i, cardValue(el, showKeyMax)), recipient)
	c.nested(el, "", indent+"  ", true)
}

// keys lists obj's keys, each on its own line with its value: the
// recipient-like ones first, all of them, with their values in full (up
// to their share), then showKeysMax others, and past those the names of
// the rest. A value that is an object or a list names the keys in it, and
// shows the recipient-like ones among them in full, at any depth, as do
// the values of the keys past showKeysMax.
func (c *card) keys(obj map[string]interface{}, indent string) {
	if len(obj) == 0 {
		c.put(" {}", false)
		return
	}
	keys, recipients := c.order(obj)
	share := showRecipientMax
	if recipients > showRecipientsFew {
		share = showRecipientMin
	}
	by, pair := c.pair(obj)
	for i, k := range keys {
		if i < recipients {
			name := cardValue(k, showValueMax)
			if by != "" && k != by && !c.recipient(k) {
				name += " (" + cardValue(pair, showNameMax) + ")"
			}
			c.fullValue(name, obj[k], indent, share)
			continue
		}
		if i-recipients == showKeysMax {
			c.put(fmt.Sprintf("\n%s+%d more keys: %s", indent, len(keys)-i, nameList(keys[i:], true)), false)
			for _, rest := range keys[i:] {
				c.nested(obj[rest], rest, indent, false)
			}
			return
		}
		c.put("\n"+indent+cardValue(k, showNameMax)+": "+cardValue(obj[k], showKeyMax), false)
		c.nested(obj[k], "", indent+"  ", true)
	}
}

// fullValue writes a recipient-like value under name: up to share bytes of
// a string; a list's elements up to share bytes, at least its first
// showListHead and always its last, and for what it leaves out how many
// and their domains; an object's keys. It records where the text shows the
// outermost such value, for the net.
func (c *card) fullValue(name string, v interface{}, indent string, share int) {
	if !c.inField {
		c.inField = true
		defer func(start int) {
			c.inField = false
			c.fields = append(c.fields, [2]int{start, c.b.Len()})
		}(c.b.Len())
	}
	switch x := v.(type) {
	case map[string]interface{}:
		c.put("\n"+indent+name+":", true)
		c.keys(x, indent+"  ")
		return
	case []interface{}:
		c.put("\n"+indent+name+":", true)
		if len(x) == 0 {
			c.put(" []", true)
			return
		}
		used, last := 0, len(x)-1
		for i := 0; i < len(x); i++ {
			s := cardValue(x[i], showKeyMax)
			if i >= showListHead && i < last && used+len(s) > share {
				c.put(fmt.Sprintf("\n%s  … %s", indent, leftOut(x[i:last])), true)
				i = last - 1
				continue
			}
			used += len(s)
			c.element(i, x[i], indent+"  ", true)
		}
		return
	}
	raw, _ := json.Marshal(v)
	s := string(raw)
	if len(s) <= share {
		c.put("\n"+indent+name+": "+escapeHidden(s), true)
		return
	}
	i, j := runeIndex(s, share*3/4), runeIndex(s, len(s)-share/4)
	// The domains of the words the cut runs through count whole.
	lo, hi := i, j
	for lo > 0 && !domainSep(rune(s[lo-1])) {
		lo--
	}
	for hi < len(s) && !domainSep(rune(s[hi])) {
		hi++
	}
	c.put(fmt.Sprintf("\n%s%s: %s … %d more bytes%s … %s", indent, name, escapeHidden(s[:i]), j-i, domainsIn(s[lo:hi]), escapeHidden(s[j:])), true)
}

// nested names the keys inside v, an object or a list, under its line
// (when names): to showDepthMax levels, as paths ("b.to", "[1].to")
// under prefix, the recipient-like ones first and at any depth, up to
// showNamesMax bytes; then it writes each recipient-like one's value in
// full.
func (c *card) nested(v interface{}, prefix, indent string, names bool) {
	type found struct {
		path  string
		value interface{}
	}
	var recipients []found
	var recipientPaths, others []string
	var walk func(v interface{}, prefix string, depth int)
	walk = func(v interface{}, prefix string, depth int) {
		switch x := v.(type) {
		case map[string]interface{}:
			by, pair := c.pair(x)
			for _, k := range sortedKeys(x) {
				path := joinPath(prefix, k)
				switch {
				case by != "" && k != by && !c.recipient(k):
					recipientPaths = append(recipientPaths, path)
					recipients = append(recipients, found{cardValue(path, showValueMax) + " (" + cardValue(pair, showNameMax) + ")", x[k]})
				case c.recipient(k):
					recipientPaths = append(recipientPaths, path)
					recipients = append(recipients, found{cardValue(path, showValueMax), x[k]})
				case depth <= showDepthMax:
					others = append(others, path)
				}
				walk(x[k], path, depth+1)
			}
		case []interface{}:
			for i, el := range x {
				walk(el, fmt.Sprintf("%s[%d]", prefix, i), depth+1)
			}
		}
	}
	walk(v, prefix, 1)
	if all := append(recipientPaths, others...); names && len(all) > 0 {
		c.put("\n"+indent+"keys: "+nameList(all, true), len(recipientPaths) > 0)
	}
	share := showRecipientMax
	if len(recipients) > showRecipientsFew {
		share = showRecipientMin
	}
	for _, f := range recipients {
		c.fullValue(f.path, f.value, indent, share)
	}
}

func joinPath(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
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
