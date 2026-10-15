package httpnodes

import (
	"fmt"
	"strconv"
	"strings"
)

// splitShellWords splits a POSIX-shell command line into words: single
// quotes, double quotes (with \" \\ \$ \` escapes), bash $'...' strings,
// backslash escapes and backslash-newline continuations. A caret before a
// newline (Windows cmd's continuation) is treated the same way.
func splitShellWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	rs := []rune(s)
	flush := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case (r == '\\' || r == '^') && i+1 < len(rs) && (rs[i+1] == '\n' || (rs[i+1] == '\r' && i+2 < len(rs) && rs[i+2] == '\n')):
			// Line continuation.
			if rs[i+1] == '\r' {
				i++
			}
			i++
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		case r == '\\':
			inWord = true
			if i+1 < len(rs) {
				i++
				cur.WriteRune(rs[i])
			}
		case r == '\'':
			inWord = true
			end := indexRune(rs, i+1, '\'')
			if end < 0 {
				return nil, fmt.Errorf("curl: unterminated single quote")
			}
			cur.WriteString(string(rs[i+1 : end]))
			i = end
		case r == '$' && i+1 < len(rs) && rs[i+1] == '\'':
			inWord = true
			n, err := readANSIC(rs, i+2, &cur)
			if err != nil {
				return nil, err
			}
			i = n
		case r == '"':
			inWord = true
			j := i + 1
			for ; j < len(rs) && rs[j] != '"'; j++ {
				if rs[j] == '\\' && j+1 < len(rs) {
					switch rs[j+1] {
					case '"', '\\', '$', '`':
						j++
						cur.WriteRune(rs[j])
						continue
					case '\n':
						j++
						continue
					}
				}
				cur.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, fmt.Errorf("curl: unterminated double quote")
			}
			i = j
		default:
			inWord = true
			cur.WriteRune(r)
		}
	}
	flush()
	return words, nil
}

func indexRune(rs []rune, from int, r rune) int {
	for i := from; i < len(rs); i++ {
		if rs[i] == r {
			return i
		}
	}
	return -1
}

// readANSIC reads a $'...' body starting at i and returns the index of the
// closing quote.
func readANSIC(rs []rune, i int, out *strings.Builder) (int, error) {
	for ; i < len(rs); i++ {
		r := rs[i]
		if r == '\'' {
			return i, nil
		}
		if r != '\\' || i+1 >= len(rs) {
			out.WriteRune(r)
			continue
		}
		i++
		switch rs[i] {
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case 'r':
			out.WriteByte('\r')
		case '0':
			out.WriteByte(0)
		case 'x', 'u', 'U':
			width := map[rune]int{'x': 2, 'u': 4, 'U': 8}[rs[i]]
			j := i + 1
			for j < len(rs) && j < i+1+width && strings.ContainsRune("0123456789abcdefABCDEF", rs[j]) {
				j++
			}
			if j == i+1 {
				out.WriteRune('\\')
				out.WriteRune(rs[i])
				continue
			}
			n, _ := strconv.ParseUint(string(rs[i+1:j]), 16, 32)
			if rs[i] == 'x' {
				out.WriteByte(byte(n))
			} else {
				out.WriteRune(rune(n))
			}
			i = j - 1
		default:
			// \\ \' \" \? and anything else: the character itself.
			out.WriteRune(rs[i])
		}
	}
	return 0, fmt.Errorf("curl: unterminated $'...' string")
}
