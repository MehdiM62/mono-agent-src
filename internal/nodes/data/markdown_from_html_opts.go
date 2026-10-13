package data

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Options of html_to_markdown, after n8n's Markdown node (which uses the
// node-html-markdown library). Not implemented: node-html-markdown's
// per-element translator overrides and its preferNativeParser switch.
// heading_style (atx|setext) is a mono addition.
type htmlToMarkdownOptions struct {
	BulletMarker                string
	CodeBlockStyle              string // fence | indented
	CodeFence                   string
	EmDelimiter                 string
	StrongDelimiter             string
	HeadingStyle                string // atx | setext
	Ignore                      map[string]bool
	BlockElements               map[string]bool
	KeepDataImages              bool
	MaxConsecutiveNewlines      int
	UseLinkReferenceDefinitions bool
	TextReplace                 []regexReplacement
	LineStartEscape             *regexReplacement // nil: built-in rule
	GlobalEscape                *regexReplacement // nil: built-in rule
}

// regexReplacement is a {pattern, replacement} pair. The replacement uses
// JavaScript syntax ($&, $1, $$) as in n8n and is translated for Go.
type regexReplacement struct {
	re   *regexp.Regexp
	repl string
}

func (r regexReplacement) apply(s string) string { return r.re.ReplaceAllString(s, r.repl) }

func parseHTMLToMarkdownOptions(config map[string]interface{}) (htmlToMarkdownOptions, error) {
	o := htmlToMarkdownOptions{
		BulletMarker:                cfgString(config, "bullet_marker", "*"),
		CodeBlockStyle:              cfgString(config, "code_block_style", "fence"),
		CodeFence:                   cfgString(config, "code_fence", "```"),
		EmDelimiter:                 cfgString(config, "em_delimiter", "_"),
		StrongDelimiter:             cfgString(config, "strong_delimiter", "**"),
		HeadingStyle:                cfgString(config, "heading_style", "atx"),
		Ignore:                      lowerSet(cfgNames(config, "ignore")),
		BlockElements:               lowerSet(cfgNames(config, "block_elements")),
		KeepDataImages:              cfgBool(config, "keep_data_images", false),
		UseLinkReferenceDefinitions: cfgBool(config, "use_link_reference_definitions", false),
	}
	if o.CodeBlockStyle == "fenced" {
		o.CodeBlockStyle = "fence"
	}
	if o.CodeBlockStyle != "fence" && o.CodeBlockStyle != "indented" {
		return o, fmt.Errorf("%w: code_block_style: %q is not fence or indented", workflow.ErrInvalidConfig, o.CodeBlockStyle)
	}
	if o.HeadingStyle != "atx" && o.HeadingStyle != "setext" {
		return o, fmt.Errorf("%w: heading_style: %q is not atx or setext", workflow.ErrInvalidConfig, o.HeadingStyle)
	}
	n, err := cfgInt(config, "max_consecutive_newlines", 3)
	if err != nil {
		return o, fmt.Errorf("%w: %v", workflow.ErrInvalidConfig, err)
	}
	if n < 1 {
		n = 1
	}
	o.MaxConsecutiveNewlines = n

	list, err := cfgReplacementList(config, "text_replace")
	if err != nil {
		return o, err
	}
	o.TextReplace = list
	if o.LineStartEscape, err = cfgReplacement(config, "line_start_escape"); err != nil {
		return o, err
	}
	if o.GlobalEscape, err = cfgReplacement(config, "global_escape"); err != nil {
		return o, err
	}
	return o, nil
}

func lowerSet(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[strings.ToLower(n)] = true
	}
	return m
}

// cfgReplacement reads one {pattern, replacement} object (also n8n's
// {"value": {...}} wrapper). An empty pattern means "not set".
func cfgReplacement(config map[string]interface{}, key string) (*regexReplacement, error) {
	obj, err := cfgObject(config, key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", workflow.ErrInvalidConfig, err)
	}
	if inner, ok := obj["value"].(map[string]interface{}); ok {
		obj = inner
	}
	return newRegexReplacement(obj, key)
}

// cfgReplacementList reads a list of {pattern, replacement} objects (also
// n8n's {"values": [...]} wrapper).
func cfgReplacementList(config map[string]interface{}, key string) ([]regexReplacement, error) {
	if obj, ok := config[key].(map[string]interface{}); ok {
		config = map[string]interface{}{key: obj["values"]}
	}
	list, err := cfgList(config, key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", workflow.ErrInvalidConfig, err)
	}
	var out []regexReplacement
	for i, raw := range list {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%w: %s[%d]: expected an object with pattern and replacement", workflow.ErrInvalidConfig, key, i)
		}
		r, err := newRegexReplacement(m, fmt.Sprintf("%s[%d]", key, i))
		if err != nil {
			return nil, err
		}
		if r != nil {
			out = append(out, *r)
		}
	}
	return out, nil
}

func newRegexReplacement(m map[string]interface{}, field string) (*regexReplacement, error) {
	pattern := cfgString(m, "pattern", "")
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: %s.pattern: %v", workflow.ErrInvalidConfig, field, err)
	}
	return &regexReplacement{re: re, repl: jsReplacementToGo(cfgString(m, "replacement", ""))}, nil
}

// jsReplacementToGo translates a JavaScript replacement string ($&, $1,
// $$) into regexp.Expand syntax (${0}, ${1}, $$).
func jsReplacementToGo(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '$' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		next := s[i+1]
		switch {
		case next == '$':
			b.WriteString("$$")
			i++
		case next == '&':
			b.WriteString("${0}")
			i++
		case next >= '0' && next <= '9':
			j := i + 1
			for j < len(s) && j < i+3 && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			b.WriteString("${" + s[i+1:j] + "}")
			i = j - 1
		default:
			b.WriteString("$$")
		}
	}
	return b.String()
}

// Built-in escaping: characters with Markdown meaning inside text, and
// line starts that would turn a paragraph into a heading, quote, list or
// rule.
var (
	mdGlobalEscapeRe    = regexp.MustCompile("[\\\\`*_~\\[\\]]")
	mdLineStartEscapeRe = regexp.MustCompile(`(?m)^(\s*)(?:(#{1,6}\s|>|[-+=]\s?)|(\d+)(\.\s))`)
)

func (o htmlToMarkdownOptions) escapeText(s string) string {
	if o.GlobalEscape != nil {
		s = o.GlobalEscape.apply(s)
	} else {
		s = mdGlobalEscapeRe.ReplaceAllString(s, `\${0}`)
	}
	for _, r := range o.TextReplace {
		s = r.apply(s)
	}
	return s
}

func (o htmlToMarkdownOptions) escapeLineStart(s string) string {
	if o.LineStartEscape != nil {
		return o.LineStartEscape.apply(s)
	}
	return mdLineStartEscapeRe.ReplaceAllString(s, `${1}${3}\${2}${4}`)
}
