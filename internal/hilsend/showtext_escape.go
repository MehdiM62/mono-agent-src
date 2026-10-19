package hilsend

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

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

// runeIndex is i, or the start of the rune i falls in.
func runeIndex(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
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

// OneLine is s with what breaks or hides a line escaped (escapeHidden),
// for a terminal that prints a card's text a line at a time. It leaves an
// escaped value as it is.
func OneLine(s string) string { return escapeHidden(s) }

func hiddenRune(r rune) bool {
	switch {
	case unicode.IsControl(r), unicode.In(r, unicode.Cf, unicode.Co, unicode.Zl, unicode.Zp, unicode.Variation_Selector):
		return true
	case unicode.Is(unicode.Zs, r):
		return r != ' '
	}
	return !unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S, unicode.Z) // unassigned
}
