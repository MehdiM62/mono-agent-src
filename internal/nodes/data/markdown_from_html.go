package data

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// htmlToMarkdown converts an HTML document or fragment to Markdown
// (CommonMark with GFM tables and strikethrough).
func htmlToMarkdown(src string, o htmlToMarkdownOptions) (string, error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", fmt.Errorf("parse HTML: %w", err)
	}
	c := &mdConverter{opts: o, refIndex: map[string]int{}}
	w := newTextWriter()
	c.children(w, doc, mdContext{})
	out := strings.Trim(w.String(), "\n")
	if len(c.refs) > 0 {
		var b strings.Builder
		b.WriteString(out)
		b.WriteString("\n\n")
		for i, href := range c.refs {
			fmt.Fprintf(&b, "[%d]: %s\n", i+1, href)
		}
		out = strings.TrimRight(b.String(), "\n")
	}
	return collapseNewlines(out, o.MaxConsecutiveNewlines), nil
}

type mdConverter struct {
	opts     htmlToMarkdownOptions
	refs     []string
	refIndex map[string]int
}

// mdContext carries where we are in the tree.
type mdContext struct {
	inList  bool // directly inside a list item (nested lists hug their parent)
	inTable bool // inside a table cell: no block structure, no raw newlines
}

// mdSkipped are elements never rendered.
var mdSkipped = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"head": true, "title": true, "meta": true, "link": true,
}

// mdBlocks maps block elements to the blank-line spacing around them.
var mdBlocks = map[string]int{
	"p": 2, "div": 1, "section": 1, "article": 1, "header": 1, "footer": 1,
	"main": 1, "nav": 1, "aside": 1, "figure": 1, "figcaption": 1,
	"form": 1, "fieldset": 1, "address": 1, "details": 1, "summary": 1,
	"dl": 2, "dt": 1, "dd": 1, "center": 1, "caption": 1,
}

func (c *mdConverter) children(w *textWriter, n *html.Node, ctx mdContext) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.node(w, ch, ctx)
	}
}

// render converts n's children into a fresh writer and returns the text.
func (c *mdConverter) render(n *html.Node, ctx mdContext) string {
	w := newTextWriter()
	c.children(w, n, ctx)
	return strings.Trim(w.String(), "\n")
}

func (c *mdConverter) node(w *textWriter, n *html.Node, ctx mdContext) {
	switch n.Type {
	case html.TextNode:
		c.textNode(w, n.Data)
		return
	case html.DocumentNode:
		c.children(w, n, ctx)
		return
	case html.ElementNode:
	default:
		return
	}
	tag := n.Data
	if mdSkipped[tag] || c.opts.Ignore[tag] {
		return
	}
	switch tag {
	case "br":
		if ctx.inTable {
			w.inline("<br>")
		} else {
			w.lineBreak("  ")
		}
	case "hr":
		w.block("---", 2, 2)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		c.heading(w, n, ctx, int(tag[1]-'0'))
	case "blockquote":
		body := c.render(n, mdContext{})
		lines := strings.Split(body, "\n")
		for i, l := range lines {
			if l == "" {
				lines[i] = ">"
			} else {
				lines[i] = "> " + l
			}
		}
		w.block(strings.Join(lines, "\n"), 2, 2)
	case "ul", "ol":
		c.list(w, n, ctx)
	case "pre":
		c.codeBlock(w, n)
	case "code", "kbd", "samp", "tt":
		c.inlineCode(w, nodeText(n))
	case "em", "i", "cite", "var", "dfn":
		c.wrapInline(w, n, ctx, c.opts.EmDelimiter)
	case "strong", "b":
		c.wrapInline(w, n, ctx, c.opts.StrongDelimiter)
	case "del", "s", "strike":
		c.wrapInline(w, n, ctx, "~~")
	case "a":
		c.link(w, n, ctx)
	case "img":
		c.image(w, n)
	case "table":
		c.table(w, n)
	default:
		lb, isBlock := mdBlocks[tag]
		if c.opts.BlockElements[tag] {
			lb, isBlock = 2, true
		}
		if isBlock && !ctx.inTable {
			w.breaks(lb)
			c.children(w, n, ctx)
			w.breaks(lb)
			return
		}
		c.children(w, n, ctx)
	}
}

func (c *mdConverter) textNode(w *textWriter, s string) {
	if strings.TrimFunc(s, isCollapsibleSpace) == "" {
		if s != "" {
			w.space()
		}
		return
	}
	escaped := c.opts.escapeText(s)
	if w.willStartLine() {
		escaped = c.opts.escapeLineStart(strings.TrimLeftFunc(escaped, isCollapsibleSpace))
	}
	w.text(escaped)
}

func (c *mdConverter) heading(w *textWriter, n *html.Node, ctx mdContext, level int) {
	body := strings.Join(strings.Fields(strings.ReplaceAll(c.render(n, mdContext{inTable: true}), "<br>", " ")), " ")
	if ctx.inTable {
		w.text(body)
		return
	}
	if body == "" {
		return
	}
	if c.opts.HeadingStyle == "setext" && level <= 2 {
		underline := "="
		if level == 2 {
			underline = "-"
		}
		w.block(body+"\n"+strings.Repeat(underline, max(3, len([]rune(body)))), 2, 2)
		return
	}
	w.block(strings.Repeat("#", level)+" "+body, 2, 2)
}

func (c *mdConverter) list(w *textWriter, n *html.Node, ctx mdContext) {
	ordered := n.Data == "ol"
	num := 1
	if ordered {
		if s, err := strconv.Atoi(htmlAttr(n, "start")); err == nil {
			num = s
		}
	}
	space := 2
	if ctx.inList {
		space = 1
	}
	w.breaks(space)
	for li := n.FirstChild; li != nil; li = li.NextSibling {
		if li.Type != html.ElementNode {
			continue
		}
		if li.Data == "ul" || li.Data == "ol" { // stray nested list
			c.list(w, li, mdContext{inList: true})
			continue
		}
		if li.Data != "li" {
			continue
		}
		marker := c.opts.BulletMarker + " "
		if ordered {
			marker = strconv.Itoa(num) + ". "
			num++
		}
		body := c.render(li, mdContext{inList: true})
		w.block(prefixLines(body, marker, strings.Repeat(" ", len(marker))), 1, 1)
	}
	w.breaks(space)
}

var mdLanguageClass = regexp.MustCompile(`(?:^|\s)(?:language|lang)-([^\s]+)`)

func (c *mdConverter) codeBlock(w *textWriter, n *html.Node) {
	code := strings.TrimSuffix(nodeText(n), "\n")
	lang := ""
	for _, el := range []*html.Node{n, firstElementChild(n, "code")} {
		if el == nil {
			continue
		}
		if m := mdLanguageClass.FindStringSubmatch(htmlAttr(el, "class")); m != nil {
			lang = m[1]
		}
	}
	if c.opts.CodeBlockStyle == "indented" {
		lines := strings.Split(code, "\n")
		for i, l := range lines {
			lines[i] = "    " + l
		}
		w.block(strings.Join(lines, "\n"), 2, 2)
		return
	}
	fence := c.opts.CodeFence
	for strings.Contains(code, fence) {
		fence += fence[:1]
	}
	w.block(fence+lang+"\n"+code+"\n"+fence, 2, 2)
}

func (c *mdConverter) inlineCode(w *textWriter, code string) {
	if code == "" {
		return
	}
	code = strings.ReplaceAll(code, "\n", " ")
	longest, run := 0, 0
	for _, r := range code {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	delim := strings.Repeat("`", longest+1)
	if strings.HasPrefix(code, "`") || strings.HasSuffix(code, "`") {
		code = " " + code + " "
	}
	w.inline(delim + code + delim)
}

// wrapInline renders n's content between delim, keeping surrounding
// whitespace outside the delimiters so the emphasis stays valid.
func (c *mdConverter) wrapInline(w *textWriter, n *html.Node, ctx mdContext, delim string) {
	raw := nodeText(n)
	body := c.render(n, mdContext{inTable: true})
	if strings.TrimSpace(body) == "" {
		if raw != "" {
			w.space()
		}
		return
	}
	if strings.IndexFunc(raw, isCollapsibleSpace) == 0 {
		w.space()
	}
	w.inline(delim + strings.TrimSpace(body) + delim)
	if r := []rune(raw); len(r) > 0 && isCollapsibleSpace(r[len(r)-1]) {
		w.space()
	}
}

func (c *mdConverter) link(w *textWriter, n *html.Node, ctx mdContext) {
	href := strings.TrimSpace(htmlAttr(n, "href"))
	if href == "" {
		c.children(w, n, ctx)
		return
	}
	text := strings.Join(strings.Fields(c.render(n, mdContext{inTable: true})), " ")
	if text == "" {
		text = c.opts.escapeText(href)
	}
	if c.opts.UseLinkReferenceDefinitions {
		idx, ok := c.refIndex[href]
		if !ok {
			c.refs = append(c.refs, href)
			idx = len(c.refs)
			c.refIndex[href] = idx
		}
		w.inline(fmt.Sprintf("[%s][%d]", text, idx))
		return
	}
	w.inline("[" + text + "](" + mdDestination(href) + mdTitle(n) + ")")
}

func (c *mdConverter) image(w *textWriter, n *html.Node) {
	src := strings.TrimSpace(htmlAttr(n, "src"))
	if src == "" || (!c.opts.KeepDataImages && strings.HasPrefix(strings.ToLower(src), "data:")) {
		return
	}
	alt := c.opts.escapeText(strings.Join(strings.Fields(htmlAttr(n, "alt")), " "))
	w.inline("![" + alt + "](" + mdDestination(src) + mdTitle(n) + ")")
}

// mdDestination writes a link destination, using <…> when it has spaces
// or parentheses.
func mdDestination(href string) string {
	if strings.ContainsAny(href, " ()<>") {
		return "<" + strings.NewReplacer("<", "%3C", ">", "%3E").Replace(href) + ">"
	}
	return href
}

func mdTitle(n *html.Node) string {
	t := htmlAttr(n, "title")
	if t == "" {
		return ""
	}
	return ` "` + strings.ReplaceAll(t, `"`, `\"`) + `"`
}

// nodeText is the concatenated text content of n.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			b.WriteString(x.Data)
		}
		if x.Type == html.ElementNode && x.Data == "br" {
			b.WriteByte('\n')
		}
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

func firstElementChild(n *html.Node, tag string) *html.Node {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && ch.Data == tag {
			return ch
		}
	}
	return nil
}

// collapseNewlines limits runs of newlines to max.
func collapseNewlines(s string, max int) string {
	limit := strings.Repeat("\n", max)
	for strings.Contains(s, limit+"\n") {
		s = strings.ReplaceAll(s, limit+"\n", limit)
	}
	return s
}
