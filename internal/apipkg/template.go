package apipkg

import (
	"fmt"
	"github.com/google/uuid"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Scope is what templates read: p (the op params), cred (the credential),
// item (the current input item's JSON), el (the element a $map is on) and
// res (the main request's decoded response, in "also" requests).
type Scope struct {
	P    map[string]any
	Cred map[string]any
	Item map[string]any
	El   any
	Res  any
	// Items is every input item's JSON, set for batch operations.
	Items []any
	// Pre is the decoded response of the op's (last) "before" request;
	// Pres holds every before step's response in order.
	Pre  any
	Pres []any
	// Bin describes the upload's binary property: file_name, mime_type,
	// file_extension (set when the op uploads a file).
	Bin map[string]any
	// Main is the main request's decoded response, unchanged while poll
	// repeats rebind res (Dropcontact: the id only the first reply has).
	Main any
	// Hdr is the main response's headers (first value each); lookups are
	// case-insensitive (hdr.Location, hdr.Resource-ID).
	Hdr headerMap
	// Idx is the element's position inside a $map / $entries.
	Idx int
	// Page holds the page params when pagination.param_in is "path"
	// ({{ page.pageNumber }}); Req describes the built request for
	// inject.hmac (method, path, query, body).
	Page map[string]any
	Req  map[string]any
	// Up is the enclosing element inside a nested $map (the outer el).
	Up any
	// Batch lists the chunk's items as {p, item, index} in a bulk request.
	Batch []any
	// Hook is the app webhook in a package's webhook block: url, path,
	// secret, id, workflow_id, node_id and extra (see webhook.go).
	Hook map[string]any
}

// headerMap holds response headers; lookupPath matches its keys
// case-insensitively.
type headerMap map[string]any

func (s Scope) root(name string) any {
	switch name {
	case "p":
		return s.P
	case "cred":
		return s.Cred
	case "item":
		return s.Item
	case "el":
		return s.El
	case "up":
		return s.Up
	case "res":
		return s.Res
	case "items":
		return s.Items
	case "pre":
		return s.Pre
	case "pres":
		return s.Pres
	case "bin":
		return s.Bin
	case "main":
		return s.Main
	case "hdr":
		return s.Hdr
	case "idx":
		return s.Idx
	case "page":
		return s.Page
	case "req":
		return s.Req
	case "batch":
		return s.Batch
	case "hook":
		return s.Hook
	case "now":
		return time.Now().UTC().Format(time.RFC3339)
	case "uuid":
		return uuid.NewString()
	}
	return nil
}

// tmpl is a parsed template string: literal text and {{ … }} expressions.
type tmpl struct {
	src    string
	parts  []tmplPart
	single bool // the whole string is exactly one expression
}

type tmplPart struct {
	lit  string
	expr *expr
}

// expr is "root.path | filter | filter:arg".
type expr struct {
	src     string
	root    string
	path    []string
	filters []filterCall
	// head is a parenthesised sub-expression the value starts from:
	// {{ (p.a | lower) | prefix:'x' }}.
	head *expr
}

type filterCall struct {
	name string
	arg  *filterArg
	// args holds every argument of a multi-argument filter (find,
	// find_row); arg is then args[0].
	args []*filterArg
}

// multiArgFilters take comma-separated arguments, passed to applyFilter
// as a []any.
var multiArgFilters = map[string][2]int{"find": {2, 2}, "find_row": {2, 2}, "row": {1, 2}, "replace": {2, 2}, "zip": {1, 3}, "set": {2, 2}}

type filterArg struct {
	lit  any   // a quoted string, number, bool or null
	path *expr // or a path (p.x, cred.y, item.z)
	sub  *expr // or a parenthesised expression: (p.sheetName | sheetgid)
}

// argValue evaluates a filter argument.
func (a *filterArg) argValue(sc Scope) (any, error) {
	switch {
	case a.sub != nil:
		return a.sub.eval(sc)
	case a.path != nil:
		return lookupPath(sc.root(a.path.root), a.path.path), nil
	}
	return a.lit, nil
}

var tmplCache sync.Map // string → *tmpl

// hasTemplate reports whether s contains template syntax.
func hasTemplate(s string) bool { return strings.Contains(s, "{{") || strings.Contains(s, `\}}`) }

// litUnescape turns the escapes \{{ and \}} of literal text into {{ and }}.
var litUnescape = strings.NewReplacer(`\{{`, "{{", `\}}`, "}}")

// parseTemplate parses s, caching the result.
func parseTemplate(s string) (*tmpl, error) {
	if t, ok := tmplCache.Load(s); ok {
		return t.(*tmpl), nil
	}
	t := &tmpl{src: s}
	rest := s
	lit := "" // literal text not yet added, escapes kept
	for {
		i := strings.Index(rest, "{{")
		if i > 0 && rest[i-1] == '\\' {
			// \{{ is a literal {{ (Zendesk placeholders in a body).
			lit += rest[:i+2]
			rest = rest[i+2:]
			continue
		}
		if i < 0 {
			if lit+rest != "" {
				t.parts = append(t.parts, tmplPart{lit: litUnescape.Replace(lit + rest)})
			}
			break
		}
		rest = lit + rest
		i += len(lit)
		lit = ""
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			return nil, fmt.Errorf("template %q: unclosed {{", s)
		}
		if i > 0 {
			t.parts = append(t.parts, tmplPart{lit: litUnescape.Replace(rest[:i])})
		}
		e, err := parseExpr(strings.TrimSpace(rest[i+2 : i+j]))
		if err != nil {
			return nil, fmt.Errorf("template %q: %w", s, err)
		}
		t.parts = append(t.parts, tmplPart{expr: e})
		rest = rest[i+j+2:]
	}
	t.single = len(t.parts) == 1 && t.parts[0].expr != nil
	tmplCache.Store(s, t)
	return t, nil
}

// parseExpr parses the inside of {{ }}.
func parseExpr(src string) (*expr, error) {
	segs := splitPipes(src)
	var e *expr
	if h := strings.TrimSpace(segs[0]); len(h) > 1 && h[0] == '(' && h[len(h)-1] == ')' {
		inner, err := parseExpr(strings.TrimSpace(h[1 : len(h)-1]))
		if err != nil {
			return nil, err
		}
		e = &expr{head: inner}
	} else {
		var err error
		if e, err = parsePath(h); err != nil {
			return nil, err
		}
	}
	e.src = src
	for _, f := range segs[1:] {
		f = strings.TrimSpace(f)
		name, argSrc, hasArg := strings.Cut(f, ":")
		name = strings.TrimSpace(name)
		if !knownFilters[name] {
			return nil, fmt.Errorf("unknown filter %q (see FORMAT.md §5 for the list)", name)
		}
		fc := filterCall{name: name}
		if n, multi := multiArgFilters[name]; multi {
			parts := splitArgs(argSrc)
			if !hasArg || len(parts) < n[0] || len(parts) > n[1] {
				return nil, fmt.Errorf("filter %s needs %d to %d comma-separated arguments", name, n[0], n[1])
			}
			for _, ps := range parts {
				a, err := parseArg(strings.TrimSpace(ps))
				if err != nil {
					return nil, fmt.Errorf("filter %s: %w", name, err)
				}
				fc.args = append(fc.args, a)
			}
			fc.arg = fc.args[0]
		} else if hasArg {
			a, err := parseArg(strings.TrimSpace(argSrc))
			if err != nil {
				return nil, fmt.Errorf("filter %s: %w", name, err)
			}
			fc.arg = a
		} else if name == "default" || name == "then" {
			return nil, fmt.Errorf("filter %s needs an argument, e.g. %s:\"x\"", name, name)
		}
		e.filters = append(e.filters, fc)
	}
	return e, nil
}

// splitPipes splits on | outside quotes.
func splitPipes(s string) []string {
	var out []string
	var quote rune
	start, depth := 0, 0
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(':
			depth++
		case r == ')':
			depth--
		case r == '|' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func parsePath(s string) (*expr, error) {
	if s == "" {
		return nil, fmt.Errorf("empty expression")
	}
	segs := strings.Split(s, ".")
	switch segs[0] {
	case "p", "cred", "item", "el", "up", "res", "items", "pre", "pres", "bin", "main", "hdr", "uuid", "now", "idx", "page", "req", "batch", "hook":
	default:
		return nil, fmt.Errorf("expression %q must start with p., cred., item., items, el., up., res., main., hdr., pre., pres., bin., page., req., batch, hook. or be uuid / now / idx", s)
	}
	for _, sg := range segs[1:] {
		if sg == "" {
			return nil, fmt.Errorf("expression %q has an empty path segment", s)
		}
	}
	return &expr{src: s, root: segs[0], path: segs[1:]}, nil
}

func parseArg(s string) (*filterArg, error) {
	if s == "" {
		return nil, fmt.Errorf("empty argument")
	}
	if s[0] == '(' && s[len(s)-1] == ')' {
		e, err := parseExpr(strings.TrimSpace(s[1 : len(s)-1]))
		if err != nil {
			return nil, err
		}
		return &filterArg{sub: e}, nil
	}
	if (s[0] == '"' || s[0] == '\'') && len(s) >= 2 && s[len(s)-1] == s[0] {
		return &filterArg{lit: s[1 : len(s)-1]}, nil
	}
	switch s {
	case "true":
		return &filterArg{lit: true}, nil
	case "false":
		return &filterArg{lit: false}, nil
	case "null":
		return &filterArg{lit: nil}, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return &filterArg{lit: f}, nil
	}
	p, err := parsePath(s)
	if err != nil {
		return nil, fmt.Errorf("argument %q is not a quoted string, number, bool or path", s)
	}
	return &filterArg{path: p}, nil
}
