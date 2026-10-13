package data

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// HTML → readable plain text for data.html's extract_html_content
// (return_value "text"), modelled on what n8n's HTML node (v1.2) returns:
// block elements on their own lines, paragraphs and headings separated by a
// blank line, headings upper-cased, list items prefixed, links followed by
// their URL in brackets and images shown as "alt [src]". Lines are not
// word-wrapped.

// plainTextSkipped are elements whose content is never visible text.
var plainTextSkipped = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"head": true, "title": true, "meta": true, "link": true,
}

// plainTextBlocks maps block elements to the line breaks around them.
var plainTextBlocks = map[string]int{
	"p": 2, "pre": 2, "table": 2, "dl": 1, "dt": 1, "dd": 1,
	"div": 1, "section": 1, "article": 1, "header": 1, "footer": 1,
	"main": 1, "nav": 1, "aside": 1, "figure": 1, "figcaption": 1,
	"form": 1, "fieldset": 1, "address": 1, "details": 1, "summary": 1,
	"center": 1, "tr": 1, "caption": 1,
}

// htmlNodesToPlainText converts the children of n to plain text.
func htmlNodesToPlainText(n *html.Node) string {
	w := newTextWriter()
	plainTextChildren(w, n, false)
	return strings.Trim(w.String(), "\n")
}

func plainTextChildren(w *textWriter, n *html.Node, pre bool) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		plainTextNode(w, c, pre)
	}
}

func plainTextNode(w *textWriter, n *html.Node, pre bool) {
	switch n.Type {
	case html.TextNode:
		if pre {
			w.inline(n.Data)
		} else {
			w.text(n.Data)
		}
		return
	case html.ElementNode:
	case html.DocumentNode:
		plainTextChildren(w, n, pre)
		return
	default:
		return
	}
	tag := n.Data
	if plainTextSkipped[tag] {
		return
	}
	switch tag {
	case "br":
		w.lineBreak("")
	case "hr":
		w.block(strings.Repeat("-", 40), 2, 2)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		sub := newTextWriter()
		plainTextChildren(sub, n, pre)
		w.block(strings.ToUpper(strings.Trim(sub.String(), "\n")), 2, 2)
	case "blockquote":
		sub := newTextWriter()
		plainTextChildren(sub, n, pre)
		w.block(prefixLines(strings.Trim(sub.String(), "\n"), "> ", "> "), 2, 2)
	case "ul", "ol":
		plainTextList(w, n, pre)
	case "pre":
		sub := newTextWriter()
		plainTextChildren(sub, n, true)
		w.block(strings.Trim(sub.String(), "\n"), 2, 2)
	case "a":
		plainTextAnchor(w, n, pre)
	case "img":
		alt, src := htmlAttr(n, "alt"), htmlAttr(n, "src")
		switch {
		case src == "":
			w.text(alt)
		case alt == "":
			w.inline("[" + src + "]")
		default:
			w.text(alt)
			w.space()
			w.inline("[" + src + "]")
		}
	case "td", "th":
		if w.length() > 0 && !w.willStartLine() {
			w.inline("\t")
		}
		plainTextChildren(w, n, pre)
	default:
		if lb, ok := plainTextBlocks[tag]; ok {
			w.breaks(lb)
			plainTextChildren(w, n, pre)
			w.breaks(lb)
			return
		}
		plainTextChildren(w, n, pre)
	}
}

func plainTextAnchor(w *textWriter, n *html.Node, pre bool) {
	href := strings.TrimPrefix(htmlAttr(n, "href"), "mailto:")
	if strings.HasPrefix(href, "#") {
		href = ""
	}
	before := w.length()
	plainTextChildren(w, n, pre)
	if href == "" {
		return
	}
	if w.length() == before {
		w.inline(href)
		return
	}
	w.space()
	w.inline("[" + href + "]")
}

func plainTextList(w *textWriter, n *html.Node, pre bool) {
	ordered := n.Data == "ol"
	num := 1
	if ordered {
		if s, err := strconv.Atoi(htmlAttr(n, "start")); err == nil {
			num = s
		}
	}
	w.breaks(2)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "li" {
			continue
		}
		sub := newTextWriter()
		plainTextChildren(sub, c, pre)
		marker := " * "
		if ordered {
			marker = strconv.Itoa(num) + ". "
			num++
		}
		body := strings.Trim(sub.String(), "\n")
		w.block(prefixLines(body, marker, strings.Repeat(" ", len(marker))), 1, 1)
	}
	w.breaks(2)
}

// htmlAttr returns attribute key of n, or "".
func htmlAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key && a.Namespace == "" {
			return a.Val
		}
	}
	return ""
}

// htmlHasAttr reports whether n carries attribute key.
func htmlHasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key && a.Namespace == "" {
			return true
		}
	}
	return false
}
