package data

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdown_to_html: n8n's Markdown node renders with showdown; this mode
// renders with goldmark and maps showdown's options onto it. Defaults
// follow showdown: no tables / strikethrough / task lists unless enabled,
// heading ids on (showdown style: punctuation and spaces removed,
// lower-cased), raw HTML passed through (unsafe=true).
//
// Options with no goldmark equivalent (emoji, encodeEmails,
// ghMentions/ghMentionsLink, parseImgDimensions, tablesHeaderId,
// backslashEscapesHTMLTags, literalMidWordAsterisks, smartIndentationFix,
// disableForced4SpacesIndentedSublists, splitAdjacentBlockquotes) are not
// implemented; GitHub code blocks, a mandatory space after '#' and literal
// mid-word underscores are always on (CommonMark).

type markdownToHTMLOptions struct {
	Unsafe               bool
	Tables               bool
	Strikethrough        bool
	Tasklists            bool
	SimpleLineBreaks     bool
	SimplifiedAutoLink   bool
	NoHeaderID           bool
	CustomizedHeaderID   bool
	GHCompatibleHeaderID bool
	RawHeaderID          bool
	PrefixHeaderID       string
	RawPrefixHeaderID    bool
	HeaderLevelStart     int
	OpenLinksInNewWindow bool
	CompleteHTMLDocument bool
}

func parseMarkdownToHTMLOptions(config map[string]interface{}) (markdownToHTMLOptions, error) {
	start, err := cfgInt(config, "header_level_start", 1)
	if err != nil {
		return markdownToHTMLOptions{}, fmt.Errorf("%w: %v", workflow.ErrInvalidConfig, err)
	}
	if start < 1 {
		start = 1
	}
	prefix := cfgString(config, "prefix_header_id", "")
	switch v := config["prefix_header_id"].(type) {
	case bool:
		prefix = ""
		if v {
			prefix = "section-"
		}
	case string:
		if v == "true" {
			prefix = "section-"
		} else if v == "false" {
			prefix = ""
		}
	}
	return markdownToHTMLOptions{
		Unsafe:               cfgBool(config, "unsafe", true),
		Tables:               cfgBool(config, "tables", false),
		Strikethrough:        cfgBool(config, "strikethrough", false),
		Tasklists:            cfgBool(config, "tasklists", false),
		SimpleLineBreaks:     cfgBool(config, "simple_line_breaks", false),
		SimplifiedAutoLink:   cfgBool(config, "simplified_auto_link", false),
		NoHeaderID:           cfgBool(config, "no_header_id", false),
		CustomizedHeaderID:   cfgBool(config, "customized_header_id", false),
		GHCompatibleHeaderID: cfgBool(config, "gh_compatible_header_id", false),
		RawHeaderID:          cfgBool(config, "raw_header_id", false),
		PrefixHeaderID:       prefix,
		RawPrefixHeaderID:    cfgBool(config, "raw_prefix_header_id", false),
		HeaderLevelStart:     start,
		OpenLinksInNewWindow: cfgBool(config, "open_links_in_new_window", false),
		CompleteHTMLDocument: cfgBool(config, "complete_html_document", false),
	}, nil
}

func (o markdownToHTMLOptions) newConverter() goldmark.Markdown {
	var exts []goldmark.Extender
	if o.Tables {
		exts = append(exts, extension.Table)
	}
	if o.Strikethrough {
		exts = append(exts, extension.Strikethrough)
	}
	if o.Tasklists {
		exts = append(exts, extension.TaskList)
	}
	if o.SimplifiedAutoLink {
		exts = append(exts, extension.Linkify)
	}
	var parserOpts []parser.Option
	if !o.NoHeaderID {
		parserOpts = append(parserOpts, parser.WithAutoHeadingID())
	}
	parserOpts = append(parserOpts, parser.WithASTTransformers(util.Prioritized(markdownTransformer{o}, 100)))
	var rendererOpts []goldmark.Option
	var htmlOpts []renderer.Option
	if o.Unsafe {
		htmlOpts = append(htmlOpts, html.WithUnsafe())
	}
	if o.SimpleLineBreaks {
		htmlOpts = append(htmlOpts, html.WithHardWraps())
	}
	if len(htmlOpts) > 0 {
		rendererOpts = append(rendererOpts, goldmark.WithRendererOptions(htmlOpts...))
	}
	all := append([]goldmark.Option{goldmark.WithExtensions(exts...), goldmark.WithParserOptions(parserOpts...)}, rendererOpts...)
	return goldmark.New(all...)
}

// parseContext returns a fresh parse context so heading-id de-duplication
// is per document.
func (o markdownToHTMLOptions) parseContext() parser.ParseOption {
	return parser.WithContext(parser.NewContext(parser.WithIDs(&showdownIDs{opts: o, seen: map[string]int{}})))
}

// finish trims goldmark's trailing newline and wraps a complete document.
func (o markdownToHTMLOptions) finish(s string) string {
	s = strings.TrimRight(s, "\n")
	if !o.CompleteHTMLDocument {
		return s
	}
	return "<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n</head>\n<body>\n" + s + "\n</body>\n</html>"
}

var (
	customHeaderIDRe  = regexp.MustCompile(`\s*\{([^{]+?)\}\s*$`)
	nonWordRe         = regexp.MustCompile(`[^A-Za-z0-9_]`)
	ghHeaderIDPunctRe = regexp.MustCompile("[&+$,/:;=?@\"#{}|^~\\[\\]`\\\\*)(%.!'<>]")
	quoteRe           = regexp.MustCompile(`["']`)
)

// showdownIDs generates heading ids in showdown's styles.
type showdownIDs struct {
	opts markdownToHTMLOptions
	seen map[string]int
}

func (s *showdownIDs) Generate(value []byte, _ ast.NodeKind) []byte {
	title := string(value)
	if s.opts.CustomizedHeaderID {
		if m := customHeaderIDRe.FindStringSubmatch(title); m != nil {
			title = m[1]
		}
	}
	prefix := s.opts.PrefixHeaderID
	if !s.opts.RawPrefixHeaderID {
		title = prefix + title
	}
	switch {
	case s.opts.GHCompatibleHeaderID:
		title = strings.ToLower(ghHeaderIDPunctRe.ReplaceAllString(strings.ReplaceAll(title, " ", "-"), ""))
	case s.opts.RawHeaderID:
		title = strings.ToLower(quoteRe.ReplaceAllString(strings.ReplaceAll(title, " ", "-"), "-"))
	default:
		title = strings.ToLower(nonWordRe.ReplaceAllString(title, ""))
	}
	if s.opts.RawPrefixHeaderID {
		title = prefix + title
	}
	if n, ok := s.seen[title]; ok {
		s.seen[title] = n + 1
		title = fmt.Sprintf("%s-%d", title, n)
	} else {
		s.seen[title] = 1
	}
	return []byte(title)
}

func (s *showdownIDs) Put(value []byte) { s.seen[string(value)] = 1 }

// markdownTransformer applies the options that act on the parsed tree:
// header_level_start, customized_header_id and open_links_in_new_window.
type markdownTransformer struct{ o markdownToHTMLOptions }

func (t markdownTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.Heading:
			if t.o.CustomizedHeaderID {
				stripCustomHeaderID(node, source)
			}
			if t.o.HeaderLevelStart > 1 {
				node.Level += t.o.HeaderLevelStart - 1
				if node.Level > 6 {
					node.Level = 6
				}
			}
		case *ast.Link, *ast.AutoLink:
			if t.o.OpenLinksInNewWindow {
				n.SetAttributeString("target", []byte("_blank"))
				n.SetAttributeString("rel", []byte("noopener noreferrer"))
			}
		}
		return ast.WalkContinue, nil
	})
}

// stripCustomHeaderID removes a trailing "{id}" from the heading's text;
// showdownIDs.Generate already used it for the id.
func stripCustomHeaderID(h *ast.Heading, source []byte) {
	last, ok := h.LastChild().(*ast.Text)
	if !ok {
		return
	}
	seg := last.Segment
	loc := customHeaderIDRe.FindIndex(seg.Value(source))
	if loc == nil {
		return
	}
	last.Segment = seg.WithStop(seg.Start + loc[0])
}
