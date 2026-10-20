package hilsend

import (
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// numberTokens calls emit with each number in s that may be someone's,
// once each, and stops when emit returns false (and reports false). Its
// digits are groups apart by 1 to 3 spaces, tabs, newlines, dashes, dots
// or parentheses; it takes each group of 7 to 20 digits (15 after a +),
// and each run of 2 to 6 consecutive groups of 7 to 15 digits in all (555
// 123 4567, +1 (555) 010-0199), so numbers side by side are each read. A
// date (2026-10-19, 19.10.2026, 20261019T…) or an IPv4 literal (taken as
// itself) ends a run; a decimal number (3.14159265) and a group inside a
// word (ab1234567cd) are none. Fullwidth and other digits read as ASCII.
func numberTokens(s string, emit func(string) bool) bool {
	s = foldDigits(s)
	var seen map[string]bool
	out := func(t []byte) bool {
		if seen[string(t)] {
			return true
		}
		if seen == nil {
			seen = map[string]bool{}
		}
		seen[string(t)] = true
		return emit(string(t))
	}
	// The chain's last seven groups: group e is last[e%7]. A run of six
	// ending at the one before the last starts seven back.
	type group struct{ a, b int }
	var last [7]group
	var buf []byte
	n, plus, gluedBefore := 0, false, false
	// windows emits the runs of groups that end at group e; gluedAfter:
	// the chain ends there, glued to a word.
	windows := func(e int, gluedAfter bool) bool {
		d := 0
		for k := e; k >= max(0, e-5) && d <= 15; k-- {
			d += last[k%7].b - last[k%7].a
			max := 15
			if k == e && !(k == 0 && plus) {
				max = 20
			}
			if d < 7 || d > max || k == 0 && gluedBefore && gluedAfter {
				continue
			}
			buf = buf[:0]
			if k == 0 && plus {
				buf = append(buf, '+')
			}
			for i := k; i <= e; i++ {
				buf = append(buf, s[last[i%7].a:last[i%7].b]...)
			}
			if !out(buf) {
				return false
			}
		}
		return true
	}
	// A run of two groups waits for a third: alone, apart by a dot, they
	// are a decimal number.
	add := func(a, b int) bool {
		last[n%7] = group{a, b}
		switch n++; {
		case n == 3:
			return windows(0, false) && windows(1, false)
		case n > 3:
			return windows(n-2, false)
		}
		return true
	}
	flush := func() bool {
		defer func() { n = 0 }()
		if n == 0 || n == 2 && s[last[0].b:last[1].a] == "." {
			return true
		}
		r, _ := utf8.DecodeRuneInString(s[last[(n-1)%7].b:])
		return (n != 2 || windows(0, false)) && windows(n-1, glue(r))
	}
	for i := 0; i < len(s); {
		if !isDigit(s[i]) {
			i++
			continue
		}
		a := i
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		skip, ip := dateAt(s[a:]), 0
		if skip == 0 && i-a == 8 && i < len(s) && s[i] == 'T' && validDate(s[a:a+4], s[a+4:a+6], s[a+6:a+8]) {
			skip = 8
		}
		if skip == 0 {
			ip = ipv4At(s[a:])
		}
		if skip > 0 || ip > 0 || n > 0 && !numberGap(s[last[(n-1)%7].b:a]) {
			if !flush() {
				return false
			}
		}
		switch {
		case skip > 0:
			i = a + skip
		case ip > 0:
			if !out([]byte(s[a : a+ip])) {
				return false
			}
			i = a + ip
		default:
			if n == 0 {
				r, _ := utf8.DecodeLastRuneInString(s[:a])
				plus, gluedBefore = a > 0 && s[a-1] == '+', a > 0 && glue(r)
			}
			if !add(a, i) {
				return false
			}
		}
	}
	return flush()
}

// numberGap reports what may stand between a phone number's digit groups:
// 1 to 3 spaces, dashes, dots or parentheses.
func numberGap(gap string) bool {
	n := 0
	for _, r := range gap {
		if n++; n > 3 || !unicode.IsSpace(r) && !unicode.Is(unicode.Pd, r) && r != '.' && r != '(' && r != ')' {
			return false
		}
	}
	return n > 0
}

// glue reports a rune that, next to digits, makes them part of a word.
func glue(r rune) bool { return unicode.IsLetter(r) || r == '_' }

// foldDigits is s with every decimal digit (fullwidth, Arabic-Indic,
// Devanagari…) as its ASCII one.
func foldDigits(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return strings.Map(func(r rune) rune {
				if r < utf8.RuneSelf || !unicode.Is(unicode.Nd, r) {
					return r
				}
				zero := r // digits come in runs of ten from 0
				for unicode.Is(unicode.Nd, zero-1) {
					zero--
				}
				return '0' + (r-zero)%10
			}, s)
		}
	}
	return s
}

// dateAt is the length of the valid date at s's start (2026-10-19,
// 19.10.2026, 10-19-2026, a year from 1900 to 2199), 0 when there is none
// (9999-99-99 and 5551-23-45 are none).
func dateAt(s string) int {
	if len(s) < 10 || len(s) > 10 && isDigit(s[10]) {
		return 0
	}
	switch {
	case s[4] == '-' && s[7] == '-' && validDate(s[:4], s[5:7], s[8:10]):
	case (s[2] == '.' || s[2] == '-') && s[5] == s[2] && (validDate(s[6:10], s[3:5], s[:2]) || validDate(s[6:10], s[:2], s[3:5])):
	default:
		return 0
	}
	return 10
}

func validDate(year, month, day string) bool {
	y, m, d := digitsValue(year), digitsValue(month), digitsValue(day)
	return y >= 1900 && y <= 2199 && m >= 1 && m <= 12 && d >= 1 && d <= 31
}

// digitsValue is s's value when s is all digits, -1 otherwise.
func digitsValue(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return -1
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// ipv4At is the length of the IPv4 literal at s's start, 0 when there is
// none.
func ipv4At(s string) int {
	n := 0
	for groups := 1; ; groups++ {
		d := 0
		for n < len(s) && isDigit(s[n]) && d < 4 {
			n, d = n+1, d+1
		}
		if d == 0 || d > 3 {
			return 0
		}
		if groups == 4 {
			break
		}
		if n >= len(s) || s[n] != '.' {
			return 0
		}
		n++
	}
	if n < len(s) && (isDigit(s[n]) || s[n] == '.' && n+1 < len(s) && isDigit(s[n+1])) || net.ParseIP(s[:n]) == nil {
		return 0
	}
	return n
}

// idNumbers calls f with each run of 6 digits or more in s, with its minus
// sign (-1001234567890), and stops when f returns false (reporting false).
func idNumbers(s string, f func(string) bool) bool {
	s = foldDigits(s)
	for i := 0; i < len(s); {
		if !isDigit(s[i]) {
			i++
			continue
		}
		a := i
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		if i-a < 6 {
			continue
		}
		if a > 0 && s[a-1] == '-' && (a == 1 || !isAlnum(s[a-2])) {
			a--
		}
		if !f(s[a:i]) {
			return false
		}
	}
	return true
}

// keyedIDs calls f with each number of 6 digits or more that follows a key
// with "id" among its words in s ("chat_id": 123456 or "-1001234567890",
// as JSON text inside a string holds them), and stops when f returns false
// (reporting false).
func keyedIDs(s string, f func(string) bool) bool {
	s = foldDigits(s)
	space := func(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }
	for at := 0; ; {
		c := strings.IndexByte(s[at:], ':')
		if c < 0 {
			return true
		}
		c += at
		at = c + 1
		j := c
		for j > 0 && space(s[j-1]) {
			j--
		}
		if j == 0 || s[j-1] != '"' {
			continue
		}
		lo := max(0, j-1-showKeyNameMax)
		q := strings.LastIndexByte(s[lo:j-1], '"')
		if q < 0 || !idKey(s[lo+q+1:j-1]) {
			continue
		}
		v := at
		for v < len(s) && space(s[v]) {
			v++
		}
		if v < len(s) && s[v] == '"' {
			v++
		}
		e := v
		if e < len(s) && s[e] == '-' {
			e++
		}
		d := e
		for e < len(s) && isDigit(s[e]) {
			e++
		}
		if e-d >= 6 && !f(s[v:e]) {
			return false
		}
	}
}

// epoch reports 10 or 13 digits that read as a Unix time in seconds or
// milliseconds from 2001 to 2100.
func epoch(t string) bool {
	if len(t) != 10 && len(t) != 13 {
		return false
	}
	n, err := strconv.ParseUint(t, 10, 64)
	if err != nil {
		return false
	}
	if len(t) == 13 {
		n /= 1000
	}
	return n >= 1_000_000_000 && n < 4_102_444_800
}
