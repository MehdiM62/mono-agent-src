package stylebundle

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// htmlNode is one node of the subtree js/element.js serializes: an element
// (Tag, Attrs, Kids) or a text node (Text, with Len its full length when the
// page's text was cut short).
type htmlNode struct {
	Tag   string      `json:"t"`
	Attrs [][2]string `json:"a"`
	Kids  []*htmlNode `json:"c"`
	Text  *string     `json:"x"`
	Len   int         `json:"n"`
}

var (
	// dropped elements leave no trace: they carry code, not layout.
	droppedTags = setOf("script", "style", "link", "meta", "base", "noscript", "template", "title")
	// embedded documents become an empty div of the same classes.
	embeddedTags = setOf("iframe", "frame", "frameset", "object", "embed", "applet", "portal", "fencedframe")
	voidTags     = setOf("area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr")
	// text-bearing attributes keep their length, not their words.
	textAttrs = setOf("alt", "aria-label", "aria-description", "aria-valuetext", "aria-placeholder",
		"aria-roledescription", "aria-braillelabel", "aria-brailleroledescription", "aria-keyshortcuts")
	tagName  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
	attrName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// maxAttrLen caps an attribute value (a class list can be long; a value
// longer than this is not styling).
const maxAttrLen = 1000

// allowedAttr reports whether element.html keeps an attribute: class, id,
// role, type, alt and aria-*.
func allowedAttr(name string) bool {
	switch name {
	case "class", "id", "role", "type", "alt":
		return true
	}
	return strings.HasPrefix(name, "aria-") && attrName.MatchString(name)
}

// renderHTML writes the element's markup with the page's words taken out:
// every text node becomes a same-length placeholder that keeps whitespace,
// case and digits (so the layout wraps as it did), only the allowlisted
// attributes survive, and scripts, styles and embedded documents are gone.
func renderHTML(root *htmlNode, truncated bool) string {
	var b strings.Builder
	writeNode(&b, root)
	if truncated {
		b.WriteString("\n<!-- truncated: the element has more nodes than were kept -->")
	}
	return b.String()
}

func writeNode(b *strings.Builder, n *htmlNode) {
	if n == nil {
		return
	}
	if n.Text != nil {
		b.WriteString(html.EscapeString(placeholder(*n.Text, n.Len)))
		return
	}
	tag := n.Tag
	lower := strings.ToLower(tag)
	if droppedTags[lower] {
		return
	}
	embedded := embeddedTags[lower]
	if embedded || !tagName.MatchString(tag) {
		tag = "div"
	}
	b.WriteString("<" + tag)
	for _, a := range n.Attrs {
		name := strings.ToLower(a[0])
		if !allowedAttr(name) {
			continue
		}
		v := a[1]
		if textAttrs[name] {
			v = placeholder(v, 0)
		} else {
			v = stripURLQueries(v)
			if len(v) > maxAttrLen {
				v = v[:maxAttrLen]
				for !utf8.ValidString(v) {
					v = v[:len(v)-1]
				}
			}
		}
		fmt.Fprintf(b, ` %s="%s"`, name, html.EscapeString(v))
	}
	b.WriteString(">")
	if embedded {
		b.WriteString("<!-- embedded " + lower + " removed --></div>")
		return
	}
	if voidTags[lower] {
		return
	}
	for _, k := range n.Kids {
		writeNode(b, k)
	}
	b.WriteString("</" + tag + ">")
}

// placeholder replaces text with a same-length stand-in: upper case → X,
// digits → 0, whitespace kept, anything else → x. total, when longer than
// s, pads with word-like filler to that many characters.
func placeholder(s string, total int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			b.WriteRune(r)
		case unicode.IsUpper(r):
			b.WriteByte('X')
		case unicode.IsDigit(r):
			b.WriteByte('0')
		default:
			b.WriteByte('x')
		}
		n++
	}
	for ; n < total; n++ {
		if n%6 == 5 {
			b.WriteByte(' ')
		} else {
			b.WriteByte('x')
		}
	}
	return b.String()
}
