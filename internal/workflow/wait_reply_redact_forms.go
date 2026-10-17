package workflow

import (
	"encoding/base64"
	"encoding/hex"
	"sort"
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
//     the base64 alphabets (A-Z a-z 0-9 + / - _), and then the same 1 to
//     maxFiller characters repeated between every two of a form's (a token
//     joined with "-", or with "ZZ" put between its characters, is still
//     one);
//   - in either case: matching ignores case.
//
// And within one item (ReplyRedactor.Map), a token's characters spread over a
// list of strings, or 5 or more of its values of up to 64 characters each
// holding a piece of 5 characters or more of a token (or of it reversed).
//
// Anything else gets through: a template can still emit the token in forms
// this doesn't recognise (char codes, base32, a cipher, pieces padded with
// text). Nor can a process with file access be kept from a token: it can
// read the database, or sign one with the machine's trace key.

// Shortest piece hidden: of the token, reversed or base64; of its hex; of a
// token in one of several values of an item (pieceFields of them).
const (
	tokenPiece  = 8
	hexPiece    = 16
	fieldPiece  = 5
	pieceFields = 5
	maxFiller   = 6
)

// pieceValue is the longest value that counts towards pieceFields: a value
// that is mostly a piece. A piece inside a longer text is as likely chance.
const pieceValue = 64

// tokenForms holds the lowercased pieces of every form of the tokens added,
// each packed into a uint64 (a hex piece into two), and a bit filter over
// them so most characters cost no map lookup.
type tokenForms struct {
	p8     map[uint64]struct{}
	p16    map[[2]uint64]struct{}
	p5     map[uint64]struct{}
	filter [1 << 10]uint64 // 65536 bits over p8 keys and p16 low halves
	filt5  [1 << 10]uint64 // the same over p5 keys
}

func (f *tokenForms) empty() bool { return len(f.p8) == 0 }

// add takes token's forms.
func (f *tokenForms) add(token string) {
	if f.p8 == nil {
		f.p8, f.p16, f.p5 = map[uint64]struct{}{}, map[[2]uint64]struct{}{}, map[uint64]struct{}{}
	}
	for _, form := range append(base64Cores(token), token, reversed(token)) {
		form = strings.ToLower(form)
		for i := 0; i+tokenPiece <= len(form); i++ {
			v := pack(form[i : i+tokenPiece])
			f.p8[v] = struct{}{}
			setBit(&f.filter, v)
		}
	}
	for _, form := range []string{strings.ToLower(token), strings.ToLower(reversed(token))} {
		for i := 0; i+fieldPiece <= len(form); i++ {
			v := pack(form[i : i+fieldPiece])
			f.p5[v] = struct{}{}
			setBit(&f.filt5, v)
		}
	}
	h := hex.EncodeToString([]byte(token))
	for i := 0; i+hexPiece <= len(h); i++ {
		lo := pack(h[i+8 : i+16])
		f.p16[[2]uint64{pack(h[i : i+8]), lo}] = struct{}{}
		setBit(&f.filter, lo)
	}
}

func pack(s string) uint64 {
	var v uint64
	for i := 0; i < len(s); i++ {
		v = v<<8 | uint64(s[i])
	}
	return v
}

func filterBit(v uint64) (int, uint64) {
	h := (v * 0x9E3779B97F4A7C15) >> 48
	return int(h >> 6), 1 << (h & 63)
}

func setBit(filter *[1 << 10]uint64, v uint64) {
	w, b := filterBit(v)
	filter[w] |= b
}

func hasBit(filter *[1 << 10]uint64, v uint64) bool {
	w, b := filterBit(v)
	return filter[w]&b != 0
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

// alphabet maps a character of the base64 alphabets to its lowercase, and
// every other character to 0.
var alphabet = func() (t [256]byte) {
	for c := 'a'; c <= 'z'; c++ {
		t[c], t[c-'a'+'A'] = byte(c), byte(c)
	}
	for c := '0'; c <= '9'; c++ {
		t[c] = byte(c)
	}
	for _, c := range "+/-_" {
		t[c] = byte(c)
	}
	return t
}()

// formScan reads text one string at a time (several make one text: a list's
// elements) and reports where a form of a token is, in one pass with no
// allocation per character. It keeps the last ring alphabet characters, so a
// form whose characters are p apart (2 ≤ p ≤ maxFiller+1) is read back from
// them where the filler between the last two repeats the one before.
type formScan struct {
	f     *tokenForms
	k     int            // alphabet characters read
	chars [ring]byte     // the last ring of them
	at    [ring]int      // and their offsets
	last  uint64         // the last 8, packed
	prev  uint64         // the 8 before those
	piece bool           // checkPiece and a fieldPiece piece was read
	check bool           // look for fieldPiece pieces
	found func(a, b int) // a form spans text[a:b]
}

// feed reads s, which starts at offset off of the text.
func (sc *formScan) feed(s string, off int) {
	f := sc.f
	for i := 0; i < len(s); i++ {
		c := alphabet[s[i]]
		if c == 0 {
			continue
		}
		k := sc.k
		sc.k++
		sc.chars[k&(ring-1)], sc.at[k&(ring-1)] = c, off+i
		sc.prev = sc.prev<<8 | sc.last>>56
		sc.last = sc.last<<8 | uint64(c)
		if sc.check && !sc.piece && k >= fieldPiece-1 {
			if v := sc.last & (1<<(8*fieldPiece) - 1); hasBit(&f.filt5, v) {
				_, sc.piece = f.p5[v]
			}
		}
		if k >= tokenPiece-1 {
			sc.match(k, 1, sc.prev, sc.last, off+i+1)
		}
		// The filler of the last gap must be the one of the gap before. Most
		// characters are let go at once: the character before this one
		// (the filler's first) is nowhere it would have to be repeated.
		filler := sc.last >> 8 & 0xff
		x := sc.last ^ filler*0x0101010101010101
		if (x-0x0101010101010101)&^x&0x8080808080000000 == 0 && byte(sc.prev) != byte(filler) {
			continue
		}
		for p := 2; p <= maxFiller+1; p++ {
			if k < (tokenPiece-1)*p || !sc.fillerRepeats(k, p) {
				continue
			}
			var hi, lo uint64
			for j := 2*tokenPiece - 1; j >= 0; j-- {
				var b uint64
				if back := j * p; back <= k && back < ring {
					b = uint64(sc.chars[(k-back)&(ring-1)])
				}
				if j >= tokenPiece {
					hi = hi<<8 | b
				} else {
					lo = lo<<8 | b
				}
			}
			sc.match(k, p, hi, lo, off+i+1)
		}
	}
}

// ring is how many alphabet characters formScan keeps: enough for a hex
// piece at the widest step.
const ring = 128

// fillerRepeats reports whether the p-1 characters before character k are
// the p-1 before character k-p.
func (sc *formScan) fillerRepeats(k, p int) bool {
	for j := 1; j < p; j++ {
		if sc.chars[(k-j)&(ring-1)] != sc.chars[(k-j-p)&(ring-1)] {
			return false
		}
	}
	return true
}

// match reports a form ending at character k (end in the text) whose
// characters are p apart: lo its last 8, hi the 8 before.
func (sc *formScan) match(k, p int, hi, lo uint64, end int) {
	f := sc.f
	if !hasBit(&f.filter, lo) {
		return
	}
	if _, ok := f.p8[lo]; ok {
		sc.found(sc.at[(k-(tokenPiece-1)*p)&(ring-1)], end)
	}
	if back := (hexPiece - 1) * p; back <= k && back < ring {
		if _, ok := f.p16[[2]uint64{hi, lo}]; ok {
			sc.found(sc.at[(k-back)&(ring-1)], end)
		}
	}
}

// spans are the merged [start, end) stretches of s holding a form, and
// whether s, if short enough to be one of an item's values a token was cut
// across (pieceValue), holds a fieldPiece piece.
func (f *tokenForms) spans(s string) ([][2]int, bool) {
	var out [][2]int
	sc := &formScan{f: f, check: len(s) <= pieceValue, found: func(a, b int) { out = append(out, [2]int{a, b}) }}
	sc.feed(s, 0)
	return mergeSpans(out), sc.piece
}

func mergeSpans(sp [][2]int) [][2]int {
	if len(sp) < 2 {
		return sp
	}
	sort.Slice(sp, func(i, j int) bool { return sp[i][0] < sp[j][0] })
	out := sp[:1]
	for _, s := range sp[1:] {
		if k := len(out) - 1; s[0] <= out[k][1] {
			out[k][1] = max(out[k][1], s[1])
		} else {
			out = append(out, s)
		}
	}
	return out
}

// hide is s with each stretch holding a form of a token, from its first
// character to its last, replaced by ReplyTokenHidden, and whether s holds
// a fieldPiece piece.
func (f *tokenForms) hide(s string) (string, bool) {
	if f.empty() || len(s) < fieldPiece {
		return s, false
	}
	spans, piece := f.spans(s)
	return replaceSpans(s, spans), piece
}

// hidePieces is s with its fieldPiece pieces of a token replaced.
func (f *tokenForms) hidePieces(s string) string {
	var sp [][2]int
	last, k := uint64(0), 0
	var at [fieldPiece]int
	for i := 0; i < len(s); i++ {
		c := alphabet[s[i]]
		if c == 0 {
			continue
		}
		at[k%fieldPiece] = i
		last = last<<8 | uint64(c)
		if k++; k >= fieldPiece {
			if _, ok := f.p5[last&(1<<(8*fieldPiece)-1)]; ok {
				sp = append(sp, [2]int{at[k%fieldPiece], i + 1})
			}
		}
	}
	return replaceSpans(s, mergeSpans(sp))
}

// spreads reports whether the strings of list, read one after the other,
// hold a form of a token.
func (f *tokenForms) spreads(list []interface{}) bool {
	if f.empty() {
		return false
	}
	hit := false
	sc := &formScan{f: f, found: func(int, int) { hit = true }}
	for _, v := range list {
		if s, ok := v.(string); ok && len(s) <= maxReplyScan {
			sc.feed(s, 0)
		}
	}
	return hit
}

func replaceSpans(s string, spans [][2]int) string {
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(s[last:sp[0]])
		b.WriteString(ReplyTokenHidden)
		last = sp[1]
	}
	b.WriteString(s[last:])
	return b.String()
}
