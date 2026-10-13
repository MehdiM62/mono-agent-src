package apipkg

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Scope is what templates read: p (the op params), cred (the credential)
// and item (the current input item's JSON).
type Scope struct {
	P    map[string]any
	Cred map[string]any
	Item map[string]any
}

func (s Scope) root(name string) any {
	switch name {
	case "p":
		return s.P
	case "cred":
		return s.Cred
	case "item":
		return s.Item
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
}

type filterCall struct {
	name string
	arg  *filterArg
}

type filterArg struct {
	lit  any   // a quoted string, number, bool or null
	path *expr // or a path (p.x, cred.y, item.z)
}

var tmplCache sync.Map // string → *tmpl

// hasTemplate reports whether s contains template syntax.
func hasTemplate(s string) bool { return strings.Contains(s, "{{") }

// parseTemplate parses s, caching the result.
func parseTemplate(s string) (*tmpl, error) {
	if t, ok := tmplCache.Load(s); ok {
		return t.(*tmpl), nil
	}
	t := &tmpl{src: s}
	rest := s
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			if rest != "" {
				t.parts = append(t.parts, tmplPart{lit: rest})
			}
			break
		}
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			return nil, fmt.Errorf("template %q: unclosed {{", s)
		}
		if i > 0 {
			t.parts = append(t.parts, tmplPart{lit: rest[:i]})
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
	e, err := parsePath(strings.TrimSpace(segs[0]))
	if err != nil {
		return nil, err
	}
	e.src = src
	for _, f := range segs[1:] {
		f = strings.TrimSpace(f)
		name, argSrc, hasArg := strings.Cut(f, ":")
		name = strings.TrimSpace(name)
		if !knownFilters[name] {
			return nil, fmt.Errorf("unknown filter %q (known: json, csv, int, float, bool, string, lower, upper, default, rl, then)", name)
		}
		fc := filterCall{name: name}
		if hasArg {
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
	start := 0
	for i, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '|':
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
	case "p", "cred", "item":
	default:
		return nil, fmt.Errorf("expression %q must start with p., cred. or item.", s)
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

// eval evaluates the expression against sc.
func (e *expr) eval(sc Scope) (any, error) {
	v, _, err := e.evalLit(sc)
	return v, err
}

// evalLit also reports whether the value is a literal the package author
// wrote (a then/default argument), which path rendering does not escape.
func (e *expr) evalLit(sc Scope) (any, bool, error) {
	v := lookupPath(sc.root(e.root), e.path)
	literal := false
	for _, f := range e.filters {
		var arg any
		argLit := false
		if f.arg != nil {
			if f.arg.path != nil {
				arg = lookupPath(sc.root(f.arg.path.root), f.arg.path.path)
			} else {
				arg, argLit = f.arg.lit, true
			}
		}
		before := v
		var err error
		if v, err = applyFilter(f.name, v, arg); err != nil {
			return nil, false, fmt.Errorf("{{ %s }}: %w", e.src, err)
		}
		switch f.name {
		case "then":
			literal = argLit && !isEmpty(before)
		case "default":
			if isEmpty(before) {
				literal = argLit
			}
		}
	}
	return v, literal, nil
}

// lookupPath walks maps by key and arrays by index (negative from the end).
func lookupPath(v any, path []string) any {
	for _, seg := range path {
		switch c := v.(type) {
		case map[string]any:
			v = c[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return nil
			}
			if i < 0 {
				i += len(c)
			}
			if i < 0 || i >= len(c) {
				return nil
			}
			v = c[i]
		default:
			return nil
		}
	}
	return v
}

// render evaluates the template. A single-expression template keeps the
// typed value; anything else is a string. escape, when set, is applied to
// each expression's text (path segments).
func (t *tmpl) render(sc Scope, escape func(string) string) (any, error) {
	if t.single && escape == nil {
		return t.parts[0].expr.eval(sc)
	}
	var b strings.Builder
	for _, p := range t.parts {
		if p.expr == nil {
			b.WriteString(p.lit)
			continue
		}
		v, literal, err := p.expr.evalLit(sc)
		if err != nil {
			return nil, err
		}
		s := stringify(v)
		if escape != nil && !literal {
			s = escape(s)
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// renderString renders a template string (literal strings pass unchanged).
func renderString(s string, sc Scope) (any, error) {
	if !hasTemplate(s) {
		return s, nil
	}
	t, err := parseTemplate(s)
	if err != nil {
		return nil, err
	}
	return t.render(sc, nil)
}

// renderText renders s and stringifies the result.
func renderText(s string, sc Scope) (string, error) {
	v, err := renderString(s, sc)
	if err != nil {
		return "", err
	}
	return stringify(v), nil
}

// renderValue renders a JSON template value: objects drop empty keys, honour
// "!key" and "$merge"; arrays drop empty elements.
func renderValue(v any, sc Scope) (any, error) {
	switch t := v.(type) {
	case string:
		return renderString(t, sc)
	case map[string]any:
		return renderObject(t, sc)
	case []any:
		out := make([]any, 0, len(t))
		for _, el := range t {
			r, err := renderValue(el, sc)
			if err != nil {
				return nil, err
			}
			if !isEmpty(r) {
				out = append(out, r)
			}
		}
		return out, nil
	default:
		return v, nil
	}
}

// renderObject renders an object template. $merge spreads object values
// into the parent first; explicit keys then override.
func renderObject(m map[string]any, sc Scope) (map[string]any, error) {
	out := map[string]any{}
	if mv, ok := m["$merge"]; ok {
		srcs := []any{mv}
		if arr, ok := mv.([]any); ok {
			srcs = arr
		}
		for _, src := range srcs {
			r, err := renderValue(src, sc)
			if err != nil {
				return nil, fmt.Errorf("$merge: %w", err)
			}
			if isEmpty(r) {
				continue
			}
			obj, ok := r.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("$merge: %v is %T, not an object", src, r)
			}
			for k, val := range obj {
				if !isEmpty(val) {
					out[k] = val
				}
			}
		}
	}
	for k, val := range m {
		if k == "$merge" {
			continue
		}
		r, err := renderValue(val, sc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", strings.TrimPrefix(k, "!"), err)
		}
		if strings.HasPrefix(k, "!") {
			out[k[1:]] = r
			continue
		}
		if isEmpty(r) {
			continue // an empty explicit key leaves a merged value in place
		}
		out[k] = r
	}
	return out, nil
}

// isEmpty: nil, "", and empty arrays/objects are empty. 0 and false are not.
func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	}
	return false
}

// stringify formats a value for a URL, header or interpolated string.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
