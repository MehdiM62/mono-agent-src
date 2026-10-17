package workflow

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// A node's template can change a token before it lands in the run's data:
// slice it, print it as hex, split it into characters. tokenForms finds a
// known token (one ReplyRedactor hides) in these forms:
//
//   - the token itself, reversed, in hex, or in base64 (standard or URL
//     alphabet, at any of the three alignments, so a resume URL encoded
//     whole is caught as well);
//   - any piece of one of those of 8 characters or more (16 in hex): a
//     slice of the token, or each part of one cut in two;
//   - with other characters in between: matching skips everything outside
//     the base64 alphabets (A-Z a-z 0-9 + / - _), so a token joined with
//     spaces, commas or quotes, or a list of its characters, is still one;
//   - in either case: matching ignores case.
//
// Anything else gets through: pieces shorter than that apart from each
// other, a split joined with "-", "_", "+" or "/", any other encoding (char
// codes, base32, rot13, a cipher). Nor can a process with file access be kept
// from a token: it can read the database, or sign one with the machine's
// trace key.

// Shortest piece hidden: of the token, reversed or base64; of its hex.
const (
	tokenPiece = 8
	hexPiece   = 16
)

// tokenForms holds the lowercased pieces of every form of the tokens added,
// by length.
type tokenForms struct {
	pieces map[int]map[string]struct{}
}

// add takes token's forms.
func (f *tokenForms) add(token string) {
	if f.pieces == nil {
		f.pieces = map[int]map[string]struct{}{}
	}
	for _, form := range append(base64Cores(token), token, reversed(token)) {
		f.addPieces(form, tokenPiece)
	}
	f.addPieces(hex.EncodeToString([]byte(token)), hexPiece)
}

func (f *tokenForms) addPieces(form string, n int) {
	form = strings.ToLower(form)
	if f.pieces[n] == nil {
		f.pieces[n] = map[string]struct{}{}
	}
	for i := 0; i+n <= len(form); i++ {
		f.pieces[n][form[i:i+n]] = struct{}{}
	}
}

func reversed(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// base64Cores is token in base64, both alphabets, as it reads inside a longer
// encoded text at each alignment: only the characters made of the token's
// bits alone.
func base64Cores(token string) []string {
	var out []string
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		for k := 0; k < 3; k++ {
			b := enc.EncodeToString(append(make([]byte, k), token...))
			head := (8*k + 5) / 6 // characters holding bits of the k bytes before
			tail := 0
			if (8*(k+len(token)))%6 != 0 {
				tail = 1 // the last character holds bits of the byte after
			}
			out = append(out, b[head:len(b)-tail])
		}
	}
	return out
}

// skeleton is s's characters in the base64 alphabets, lowercased, with the
// offset of each in s.
func skeleton(s string) ([]byte, []int) {
	sk, at := make([]byte, 0, len(s)), make([]int, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			c += 'a' - 'A'
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '+', c == '/', c == '-', c == '_':
		default:
			continue
		}
		sk, at = append(sk, c), append(at, i)
	}
	return sk, at
}

// spans are the merged [start, end) ranges of sk that pieces cover.
func (f *tokenForms) spans(sk []byte) [][2]int {
	var out [][2]int
	for i := range sk {
		for n, set := range f.pieces {
			if i+n > len(sk) {
				continue
			}
			if _, ok := set[string(sk[i:i+n])]; !ok {
				continue
			}
			if k := len(out) - 1; k >= 0 && i <= out[k][1] {
				out[k][1] = max(out[k][1], i+n)
			} else {
				out = append(out, [2]int{i, i + n})
			}
		}
	}
	return out
}

// hide is s with each stretch holding a form of a token, from its first
// character to its last, replaced by ReplyTokenHidden.
func (f *tokenForms) hide(s string) string {
	if len(f.pieces) == 0 || len(s) < tokenPiece {
		return s
	}
	sk, at := skeleton(s)
	spans := f.spans(sk)
	if spans == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(s[last:at[sp[0]]])
		b.WriteString(ReplyTokenHidden)
		last = at[sp[1]-1] + 1
	}
	b.WriteString(s[last:])
	return b.String()
}

// holds reports whether s holds a form of a token.
func (f *tokenForms) holds(s string) bool {
	if len(f.pieces) == 0 {
		return false
	}
	sk, _ := skeleton(s)
	return f.spans(sk) != nil
}
