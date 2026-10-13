package data

import (
	"strings"
	"unicode"
)

// textWriter assembles text rendered from an HTML tree, for the HTML →
// plain text (data.html) and HTML → Markdown (data.markdown) converters.
//
// Inline text has its whitespace collapsed the way a browser does. Block
// elements ask for a number of line breaks before and after them; requests
// that meet are merged (the larger wins) rather than added, so nested
// blocks do not pile up blank lines. Nothing is emitted for a request at
// the very start of the output, and trailing requests are dropped.
type textWriter struct {
	sb           strings.Builder
	pending      int  // line breaks requested before the next content
	spacePending bool // a collapsed space waits before the next word
	atLineStart  bool
	started      bool
}

func newTextWriter() *textWriter { return &textWriter{atLineStart: true} }

// breaks requests n line breaks before the next content.
func (w *textWriter) breaks(n int) {
	if n > w.pending {
		w.pending = n
	}
}

func (w *textWriter) flush() {
	if w.pending > 0 && w.started {
		w.sb.WriteString(strings.Repeat("\n", w.pending))
		w.atLineStart = true
		w.spacePending = false
	}
	w.pending = 0
}

// space records collapsible whitespace.
func (w *textWriter) space() {
	if !w.atLineStart || w.pending > 0 {
		w.spacePending = true
	}
}

// text writes s with whitespace runs collapsed to one space.
func (w *textWriter) text(s string) {
	start := -1
	for i, r := range s {
		if isCollapsibleSpace(r) {
			if start >= 0 {
				w.inline(s[start:i])
				start = -1
			}
			w.space()
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		w.inline(s[start:])
	}
}

// inline writes s verbatim as inline content (no whitespace collapsing).
func (w *textWriter) inline(s string) {
	if s == "" {
		return
	}
	w.flush()
	if w.spacePending && !w.atLineStart {
		w.sb.WriteByte(' ')
	}
	w.spacePending = false
	w.sb.WriteString(s)
	w.atLineStart = strings.HasSuffix(s, "\n")
	w.started = true
}

// lineBreak ends the current line (a <br>), writing suffix before it.
func (w *textWriter) lineBreak(suffix string) {
	w.flush()
	w.spacePending = false
	w.sb.WriteString(suffix)
	w.sb.WriteByte('\n')
	w.atLineStart = true
	w.started = true
}

// block writes pre-rendered multi-line content as a block.
func (w *textWriter) block(s string, lead, trail int) {
	w.breaks(lead)
	if s != "" {
		w.spacePending = false
		w.flush()
		w.sb.WriteString(s)
		w.atLineStart = strings.HasSuffix(s, "\n")
		w.started = true
	}
	w.breaks(trail)
}

// willStartLine reports whether the next inline content begins a line.
func (w *textWriter) willStartLine() bool {
	return w.atLineStart || (w.pending > 0 && w.started)
}

// length is the number of bytes written so far (pending breaks excluded).
func (w *textWriter) length() int { return w.sb.Len() }

func (w *textWriter) String() string { return w.sb.String() }

// isCollapsibleSpace is HTML's notion of inter-word whitespace; a
// non-breaking space is kept as content.
func isCollapsibleSpace(r rune) bool {
	return r != ' ' && unicode.IsSpace(r)
}

// prefixLines prefixes the first line of s with first and every following
// non-empty line with rest.
func prefixLines(s, first, rest string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		switch {
		case i == 0:
			lines[i] = first + l
		case l != "":
			lines[i] = rest + l
		}
	}
	return strings.Join(lines, "\n")
}
