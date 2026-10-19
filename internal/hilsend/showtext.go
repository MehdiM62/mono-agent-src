package hilsend

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
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
	showDomainsMax    = 10        // domains named for what a recipient-like value leaves out
	showDepthMax      = 4         // levels of names listed under a value (recipient-like ones at any depth)
	showParamKeysMax  = 1_000_000 // keys and elements of one param a card checks
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
// off) is marked "(not sent)". unreviewable says why the card can't be
// approved from, "" when it can: a param too large to check, or a card cut
// where it shows a recipient.
func showCard(op string, show, exprs, unsent []string, undeclared bool, config map[string]interface{}) (text, unreviewable string) {
	c := &card{recipients: map[string]bool{}}
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
		v := structured(val)
		if n := countNodes(v, showParamKeysMax); n > showParamKeysMax {
			c.put(fmt.Sprintf("\n%s: too large to review: more than %d keys and elements", label, showParamKeysMax), true)
			if c.unreviewable == "" {
				c.unreviewable = fmt.Sprintf("%s holds more than %d keys and elements: too large to review", path, showParamKeysMax)
			}
			continue
		}
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
	if c.cut {
		c.b.WriteString("\n(card cut)")
	}
	return c.b.String(), c.unreviewable
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
	cut          bool   // something didn't fit showCardMax
	unreviewable string // why the card can't be approved from
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

// recipient reports whether key k may name whom a send reaches.
func (c *card) recipient(k string) bool {
	v, ok := c.recipients[k]
	if !ok {
		v = recipientLike(k)
		c.recipients[k] = v
	}
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
// value among them), and how many of those there are.
func (c *card) order(obj map[string]interface{}) ([]string, int) {
	keys := sortedKeys(obj)
	pair := c.pairName(obj) != ""
	var first, rest []string
	for _, k := range keys {
		if c.recipient(k) || (pair && k == "value") {
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
		if c.pairName(x) != "" {
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
	pair := c.pairName(obj)
	for i, k := range keys {
		if i < recipients {
			name := cardValue(k, showValueMax)
			if pair != "" && k == "value" {
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
// and their domains; an object's keys.
func (c *card) fullValue(name string, v interface{}, indent string, share int) {
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
	c.put(fmt.Sprintf("\n%s%s: %s … %d more bytes%s … %s", indent, name, escapeHidden(s[:i]), j-i, domainsIn(s[i:j]), escapeHidden(s[j:])), true)
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
			if p := c.pairName(x); p != "" {
				path := joinPath(prefix, "value")
				recipientPaths = append(recipientPaths, path)
				recipients = append(recipients, found{cardValue(path, showValueMax) + " (" + cardValue(p, showNameMax) + ")", x["value"]})
			}
			for _, k := range sortedKeys(x) {
				path := joinPath(prefix, k)
				switch {
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

// domainsIn names the distinct domains of the addresses (a@domain) and
// links (scheme://host) in s, up to showDomainsMax.
func domainsIn(s string) string {
	seen := map[string]bool{}
	var domains []string
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(`,;"'<>()[]{}\`, r)
	}) {
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
	shown := domains
	more := ""
	if len(shown) > showDomainsMax {
		shown, more = shown[:showDomainsMax], fmt.Sprintf(", +%d more", len(domains)-showDomainsMax)
	}
	return fmt.Sprintf(", %d distinct domains: %s%s", len(domains), escapeHidden(strings.Join(shown, ", ")), more)
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
