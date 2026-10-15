package apipkg

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// eval evaluates the expression against sc.
func (e *expr) eval(sc Scope) (any, error) {
	v, _, err := e.evalLit(sc)
	return v, err
}

// evalLit also reports whether the value is a literal the package author
// wrote (a then/default argument), which path rendering does not escape.
func (e *expr) evalLit(sc Scope) (any, bool, error) {
	var v any
	if e.head != nil {
		hv, err := e.head.eval(sc)
		if err != nil {
			return nil, false, err
		}
		v = hv
	} else {
		v = lookupPath(sc.root(e.root), e.path)
	}
	literal := false
	for _, f := range e.filters {
		var arg any
		argLit := false
		if len(f.args) > 0 {
			vals := make([]any, len(f.args))
			for i, a := range f.args {
				v, err := a.argValue(sc)
				if err != nil {
					return nil, false, err
				}
				vals[i] = v
			}
			arg = vals
		} else if f.arg != nil && f.arg.sub != nil {
			var err error
			if arg, err = f.arg.sub.eval(sc); err != nil {
				return nil, false, err
			}
		} else if f.arg != nil {
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
			literal = argLit && !unset(before)
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
	for i := 0; i < len(path); i++ {
		seg := path[i]
		switch c := v.(type) {
		case headerMap:
			var hit any
			for k, x := range c {
				if strings.EqualFold(k, seg) {
					hit = x
					break
				}
			}
			v = hit
		case map[string]any:
			if x, ok := c[seg]; ok {
				v = x
				continue
			}
			// A key containing dots ("@odata.nextLink"): join the
			// following segments, longest existing key first.
			found := false
			for j := len(path); j > i+1; j-- {
				if x, ok := c[strings.Join(path[i:j], ".")]; ok {
					v, i, found = x, j-1, true
					break
				}
			}
			if !found {
				return nil
			}
		case []any:
			n, err := strconv.Atoi(seg)
			if err != nil {
				return nil
			}
			if n < 0 {
				n += len(c)
			}
			if n < 0 || n >= len(c) {
				return nil
			}
			v = c[n]
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
		if _, pre := v.(pathValue); escape != nil && !literal && !pre {
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
		if src, ok := t["$map"]; ok {
			return uniqueBuilt(t, sc)(renderMap(src, t["$to"], sc))
		}
		if parts, ok := t["$concat"]; ok {
			return uniqueBuilt(t, sc)(renderConcat(parts, sc))
		}
		if parts, ok := t["$list"]; ok {
			// Positional lists keep every element, empty ones included
			// (Odoo execute_kw args, Notion {"past_week": {}}).
			arr, ok := parts.([]any)
			if !ok {
				return nil, fmt.Errorf("$list must be a list")
			}
			out := make([]any, len(arr))
			for i, el := range arr {
				r, err := renderValue(el, sc)
				if err != nil {
					return nil, fmt.Errorf("$list element %d: %w", i, err)
				}
				if m, ok := r.(map[string]any); ok && m == nil {
					r = map[string]any{}
				}
				out[i] = r
			}
			return out, nil
		}
		if parts, ok := t["$join"]; ok {
			return renderJoin(parts, t, sc)
		}
		if inner, ok := t["$json"]; ok {
			r, err := renderValue(inner, sc)
			if err != nil {
				return nil, fmt.Errorf("$json: %w", err)
			}
			if isEmpty(r) {
				return nil, nil
			}
			b, err := json.Marshal(r)
			if err != nil {
				return nil, fmt.Errorf("$json: %w", err)
			}
			return string(b), nil
		}
		if src, ok := t["$entries"]; ok {
			return renderEntries(src, t["$key"], t["$value"], sc)
		}
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
	// $when gates the whole object: empty/false → nil (omitted); otherwise
	// the object is kept even when every other key is empty, so marker
	// objects like Google Docs' "endOfSegmentLocation": {} survive.
	if wv, ok := m["$when"]; ok {
		w, err := renderValue(wv, sc)
		if err != nil {
			return nil, fmt.Errorf("$when: %w", err)
		}
		if !truthy(w) {
			return nil, nil
		}
	}
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
			forced := forcedKeys(src)
			for k, val := range obj {
				if !isEmpty(val) || forced[k] {
					out[k] = val
				}
			}
		}
	}
	for k, val := range m {
		if k == "$merge" || k == "$when" {
			continue
		}
		r, err := renderValue(val, sc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", strings.TrimPrefix(k, "!"), err)
		}
		if strings.HasPrefix(k, "!") {
			if r == nil && isListBuilder(val) {
				r = []any{} // a forced list builder sends [] (ClickUp assignees.rem)
			}
			out[k[1:]] = r
			continue
		}
		if gated, ok := val.(map[string]any); ok {
			if _, has := gated["$when"]; has {
				if m, ok := r.(map[string]any); ok && m != nil {
					out[k] = m // a $when object that passed is kept even when empty
				}
				continue
			}
		}
		if isEmpty(r) {
			continue // an empty explicit key leaves a merged value in place
		}
		out[k] = r
	}
	return out, nil
}

// forcedKeys names the "!key" entries of a literal $merge source, so a forced
// empty value survives the spread like it does as a direct key.
func forcedKeys(src any) map[string]bool {
	m, ok := src.(map[string]any)
	if !ok {
		return nil
	}
	var forced map[string]bool
	for k := range m {
		if strings.HasPrefix(k, "!") {
			if forced == nil {
				forced = map[string]bool{}
			}
			forced[k[1:]] = true
		}
	}
	return forced
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
	case pathValue:
		return string(t)
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
