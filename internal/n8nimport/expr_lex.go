package n8nimport

import (
	"fmt"
	"strings"
)

// tokKind classifies a token of the JavaScript subset n8n expressions use.
type tokKind int

const (
	tkEOF tokKind = iota
	tkIdent
	tkNumber
	tkString
	tkTemplate // backtick template literal; raw body kept in text
	tkRegex
	tkPunct
)

type token struct {
	kind tokKind
	text string // identifier name, punctuator, decoded string, number literal, raw template body, regex body
	num  float64
	pos  int
}

// lexer tokenizes one JavaScript expression. It is deliberately small: it
// covers what appears inside n8n's {{ }} blocks (identifiers, numbers,
// strings, template literals, regex literals and punctuators) and reports
// anything else as an error so the caller keeps the expression verbatim.
type lexer struct {
	src  string
	pos  int
	toks []token
}

// punctuators, longest first so greedy matching works.
var punctuators = []string{
	">>>=", "===", "!==", "**=", "...", "<<=", ">>=", ">>>", "&&=", "||=", "??=",
	"=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "++", "--", "+=", "-=", "*=", "/=", "%=",
	"**", "<<", ">>", "&=", "|=", "^=",
	"{", "}", "(", ")", "[", "]", ";", ",", "<", ">", "+", "-", "*", "/", "%", "&", "|", "^",
	"!", "~", "?", ":", "=", ".",
}

func isIdentStart(c byte) bool {
	return c == '$' || c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

// lexJS tokenizes src completely.
func lexJS(src string) ([]token, error) {
	lx := &lexer{src: src}
	for {
		t, err := lx.next()
		if err != nil {
			return nil, err
		}
		lx.toks = append(lx.toks, t)
		if t.kind == tkEOF {
			return lx.toks, nil
		}
	}
}

// prevIsOperand reports whether the previous token ends an operand, which
// decides whether "/" is division or the start of a regex literal.
func (lx *lexer) prevIsOperand() bool {
	if len(lx.toks) == 0 {
		return false
	}
	p := lx.toks[len(lx.toks)-1]
	switch p.kind {
	case tkIdent, tkNumber, tkString, tkTemplate, tkRegex:
		return true
	case tkPunct:
		return p.text == ")" || p.text == "]" || p.text == "}"
	}
	return false
}

func (lx *lexer) next() (token, error) {
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			lx.pos++
			continue
		}
		if strings.HasPrefix(lx.src[lx.pos:], "//") {
			for lx.pos < len(lx.src) && lx.src[lx.pos] != '\n' {
				lx.pos++
			}
			continue
		}
		if strings.HasPrefix(lx.src[lx.pos:], "/*") {
			end := strings.Index(lx.src[lx.pos+2:], "*/")
			if end < 0 {
				return token{}, fmt.Errorf("unterminated comment")
			}
			lx.pos += end + 4
			continue
		}
		break
	}
	start := lx.pos
	if lx.pos >= len(lx.src) {
		return token{kind: tkEOF, pos: start}, nil
	}
	c := lx.src[lx.pos]
	switch {
	case isIdentStart(c):
		for lx.pos < len(lx.src) && isIdentPart(lx.src[lx.pos]) {
			lx.pos++
		}
		return token{kind: tkIdent, text: lx.src[start:lx.pos], pos: start}, nil
	case c >= '0' && c <= '9' || (c == '.' && lx.pos+1 < len(lx.src) && lx.src[lx.pos+1] >= '0' && lx.src[lx.pos+1] <= '9'):
		return lx.number()
	case c == '\'' || c == '"':
		s, err := lx.quoted(c)
		return token{kind: tkString, text: s, pos: start}, err
	case c == '`':
		body, err := lx.template()
		return token{kind: tkTemplate, text: body, pos: start}, err
	case c == '/' && !lx.prevIsOperand():
		return lx.regex()
	}
	for _, p := range punctuators {
		if strings.HasPrefix(lx.src[lx.pos:], p) {
			// "?." followed by a digit is a ternary followed by a number.
			if p == "?." && lx.pos+2 < len(lx.src) && lx.src[lx.pos+2] >= '0' && lx.src[lx.pos+2] <= '9' {
				continue
			}
			lx.pos += len(p)
			return token{kind: tkPunct, text: p, pos: start}, nil
		}
	}
	return token{}, fmt.Errorf("unexpected character %q at %d", c, start)
}

func (lx *lexer) number() (token, error) {
	start := lx.pos
	if strings.HasPrefix(lx.src[lx.pos:], "0x") || strings.HasPrefix(lx.src[lx.pos:], "0X") {
		lx.pos += 2
		var v float64
		for lx.pos < len(lx.src) {
			d := hexVal(lx.src[lx.pos])
			if d < 0 {
				break
			}
			v = v*16 + float64(d)
			lx.pos++
		}
		return token{kind: tkNumber, text: lx.src[start:lx.pos], num: v, pos: start}, nil
	}
	for lx.pos < len(lx.src) {
		ch := lx.src[lx.pos]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == '_' {
			lx.pos++
			continue
		}
		if (ch == 'e' || ch == 'E') && lx.pos+1 < len(lx.src) {
			lx.pos++
			if lx.src[lx.pos] == '+' || lx.src[lx.pos] == '-' {
				lx.pos++
			}
			continue
		}
		break
	}
	text := strings.ReplaceAll(lx.src[start:lx.pos], "_", "")
	var v float64
	if _, err := fmt.Sscanf(text, "%g", &v); err != nil {
		return token{}, fmt.Errorf("bad number %q", text)
	}
	return token{kind: tkNumber, text: text, num: v, pos: start}, nil
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// quoted decodes a '…' or "…" string literal.
func (lx *lexer) quoted(q byte) (string, error) {
	lx.pos++
	var b strings.Builder
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		if c == q {
			lx.pos++
			return b.String(), nil
		}
		if c == '\\' && lx.pos+1 < len(lx.src) {
			lx.pos++
			b.WriteString(unescapeJS(lx.src[lx.pos]))
			lx.pos++
			continue
		}
		b.WriteByte(c)
		lx.pos++
	}
	return "", fmt.Errorf("unterminated string")
}

func unescapeJS(c byte) string {
	switch c {
	case 'n':
		return "\n"
	case 't':
		return "\t"
	case 'r':
		return "\r"
	case '0':
		return "\x00"
	}
	return string(c)
}

// template scans a backtick literal, returning its raw body (between the
// backticks). Nested ${ … } blocks are skipped with brace counting.
func (lx *lexer) template() (string, error) {
	lx.pos++
	start := lx.pos
	depth := 0
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch {
		case c == '\\':
			lx.pos += 2
			continue
		case depth == 0 && c == '`':
			body := lx.src[start:lx.pos]
			lx.pos++
			return body, nil
		case strings.HasPrefix(lx.src[lx.pos:], "${"):
			depth++
			lx.pos += 2
			continue
		case depth > 0 && c == '{':
			depth++
		case depth > 0 && c == '}':
			depth--
		}
		lx.pos++
	}
	return "", fmt.Errorf("unterminated template literal")
}

func (lx *lexer) regex() (token, error) {
	start := lx.pos
	lx.pos++
	inClass := false
	for lx.pos < len(lx.src) {
		c := lx.src[lx.pos]
		switch {
		case c == '\\':
			lx.pos += 2
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			body := lx.src[start+1 : lx.pos]
			lx.pos++
			fl := lx.pos
			for lx.pos < len(lx.src) && isIdentPart(lx.src[lx.pos]) {
				lx.pos++
			}
			return token{kind: tkRegex, text: body + "/" + lx.src[fl:lx.pos], pos: start}, nil
		case c == '\n':
			return token{}, fmt.Errorf("unterminated regex")
		}
		lx.pos++
	}
	return token{}, fmt.Errorf("unterminated regex")
}

// splitTemplate splits an n8n parameter expression body (the text after the
// leading "=") into literal text and {{ … }} code segments. The end of a
// segment is found by lexing the code, so "}}" inside a string or object
// literal does not end it early.
func splitTemplate(s string) ([]tmplPart, error) {
	var parts []tmplPart
	for {
		i := strings.Index(s, "{{")
		if i < 0 {
			if s != "" {
				parts = append(parts, tmplPart{text: s})
			}
			return parts, nil
		}
		if i > 0 {
			parts = append(parts, tmplPart{text: s[:i]})
		}
		rest := s[i+2:]
		end, err := findSegmentEnd(rest)
		if err != nil {
			return nil, err
		}
		parts = append(parts, tmplPart{code: rest[:end], isCode: true})
		s = rest[end+2:]
	}
}

type tmplPart struct {
	text   string
	code   string
	isCode bool
}

// findSegmentEnd returns the index of the "}}" closing a {{ segment.
func findSegmentEnd(s string) (int, error) { return scanClose(s, true) }

// findBraceEnd returns the index of the "}" closing a ${ block.
func findBraceEnd(s string) (int, error) { return scanClose(s, false) }

// scanClose finds the closing "}}" (double) or "}" at brace depth zero,
// skipping string and template literals.
func scanClose(s string, double bool) (int, error) {
	depth := 0
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			lx := &lexer{src: s, pos: i}
			var err error
			if c == '`' {
				_, err = lx.template()
			} else {
				_, err = lx.quoted(c)
			}
			if err != nil {
				return 0, err
			}
			i = lx.pos
			continue
		case c == '{':
			depth++
		case c == '}':
			if depth == 0 && (!double || (i+1 < len(s) && s[i+1] == '}')) {
				return i, nil
			}
			if depth > 0 {
				depth--
			}
		}
		i++
	}
	return 0, fmt.Errorf("unterminated block")
}
