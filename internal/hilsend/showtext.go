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
	showValueMax = 500 // bytes of a param's value
	showKeyMax   = 200 // bytes of one key's value in an object
	showKeysMax  = 20  // keys of an object listed with their values
	showElemsMax = 10  // elements of a list listed one by one
	showNameMax  = 80  // bytes of one key's name
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
// it; past showElemsMax, how many more.
func writeElements(b *strings.Builder, arr []interface{}, indent string) {
	if len(arr) == 0 {
		b.WriteString(" []")
		return
	}
	for i, el := range arr {
		if i == showElemsMax {
			fmt.Fprintf(b, "\n%s+%d more elements", indent, len(arr)-i)
			return
		}
		if obj, ok := el.(map[string]interface{}); ok {
			fmt.Fprintf(b, "\n%s[%d]:", indent, i)
			writeKeys(b, obj, indent+"  ")
			continue
		}
		fmt.Fprintf(b, "\n%s[%d]: %s", indent, i, cardValue(el, showKeyMax))
	}
}

// writeKeys lists obj's keys, sorted, each on its own line with its value;
// past showKeysMax, the names of the rest.
func writeKeys(b *strings.Builder, obj map[string]interface{}, indent string) {
	if len(obj) == 0 {
		b.WriteString(" {}")
		return
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i == showKeysMax {
			names := make([]string, 0, len(keys)-i)
			for _, n := range keys[i:] {
				names = append(names, cardValue(n, showNameMax))
			}
			fmt.Fprintf(b, "\n%s+%d more keys: %s", indent, len(keys)-i, escapeHidden(cutBytes(strings.Join(names, ", "), showValueMax)))
			return
		}
		b.WriteString("\n" + indent + cardValue(k, showNameMax) + ": " + cardValue(obj[k], showKeyMax))
	}
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
