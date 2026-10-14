package apipkg

import (
	"fmt"
	"strings"
)

// renderMap renders "$to" once per element of the list "$map" resolves to,
// with el bound to the element. Empty results are dropped; an empty or
// missing list renders nil (so the key is omitted).
func renderMap(src, to any, sc Scope) (any, error) {
	if to == nil {
		return nil, fmt.Errorf("$map needs a $to template")
	}
	list, err := renderValue(src, sc)
	if err != nil {
		return nil, fmt.Errorf("$map: %w", err)
	}
	if isEmpty(list) {
		return nil, nil
	}
	arr, ok := list.([]any)
	if !ok {
		return nil, fmt.Errorf("$map: %v is %T, not a list", src, list)
	}
	out := make([]any, 0, len(arr))
	for i, el := range arr {
		esc := sc
		esc.El, esc.Idx, esc.Up = el, i, sc.El
		r, err := renderValue(to, esc)
		if err != nil {
			return nil, fmt.Errorf("$map element %d: %w", i, err)
		}
		if !isEmpty(r) {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// renderConcat renders each part and joins them into one list: list parts
// are spread, other non-empty values appended, empty parts skipped.
func renderConcat(parts any, sc Scope) (any, error) {
	arr, ok := parts.([]any)
	if !ok {
		return nil, fmt.Errorf("$concat must be a list of templates")
	}
	var out []any
	for i, part := range arr {
		r, err := renderValue(part, sc)
		if err != nil {
			return nil, fmt.Errorf("$concat part %d: %w", i, err)
		}
		switch t := r.(type) {
		case nil:
		case []any:
			out = append(out, t...)
		default:
			if !isEmpty(t) {
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// splitArgs splits filter arguments on commas outside quotes.
func splitArgs(s string) []string {
	var out []string
	var q byte
	start, depth := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// renderEntries builds an object from a list: for each element (bound to
// el) "$key" renders the key and "$value" the value; empty keys or values
// are skipped. n8n filter rows → {"date[after]": 1704067200}.
func renderEntries(src, key, val any, sc Scope) (any, error) {
	ks, ok := key.(string)
	if !ok || ks == "" || val == nil {
		return nil, fmt.Errorf("$entries needs $key (a template) and $value")
	}
	list, err := renderValue(src, sc)
	if err != nil {
		return nil, fmt.Errorf("$entries: %w", err)
	}
	if isEmpty(list) {
		return nil, nil
	}
	arr, ok := list.([]any)
	if !ok {
		return nil, fmt.Errorf("$entries: %v is %T, not a list", src, list)
	}
	out := map[string]any{}
	for i, el := range arr {
		esc := sc
		esc.El, esc.Idx = el, i
		k, err := renderText(ks, esc)
		if err != nil {
			return nil, fmt.Errorf("$entries element %d key: %w", i, err)
		}
		v, err := renderValue(val, esc)
		if err != nil {
			return nil, fmt.Errorf("$entries element %d value: %w", i, err)
		}
		if k != "" && !isEmpty(v) {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// uniqueBuilt applies a list builder's "$unique" key to its result: true
// drops repeated elements, a path (text) compares objects by that value
// (SeaTable merges the row's assets with the uploaded ones).
func uniqueBuilt(m map[string]any, sc Scope) func(any, error) (any, error) {
	return func(v any, err error) (any, error) {
		u, ok := m["$unique"]
		if err != nil || !ok || v == nil {
			return v, err
		}
		if b, isBool := u.(bool); isBool {
			if !b {
				return v, nil
			}
			return uniqueList(v, nil)
		}
		path, err := renderText(stringify(u), sc)
		if err != nil {
			return nil, fmt.Errorf("$unique: %w", err)
		}
		return uniqueList(v, path)
	}
}

// isListBuilder reports whether a template value builds a list.
func isListBuilder(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, isMap := m["$map"]
	_, isConcat := m["$concat"]
	return isMap || isConcat
}

// renderJoin renders "$join" (a list of templates, or one template or
// builder yielding a list), drops empty parts, joins the rest with "$sep"
// and wraps them in "$prefix"/"$suffix". Nothing left → nil (omitted).
func renderJoin(parts any, m map[string]any, sc Scope) (any, error) {
	var vals []any
	if arr, ok := parts.([]any); ok {
		for i, p := range arr {
			r, err := renderValue(p, sc)
			if err != nil {
				return nil, fmt.Errorf("$join part %d: %w", i, err)
			}
			vals = append(vals, r)
		}
	} else {
		r, err := renderValue(parts, sc)
		if err != nil {
			return nil, fmt.Errorf("$join: %w", err)
		}
		if l, ok := r.([]any); ok {
			vals = l
		} else if r != nil {
			vals = []any{r}
		}
	}
	var keep []string
	for _, v := range vals {
		if s := stringify(v); !isEmpty(v) && s != "" {
			keep = append(keep, s)
		}
	}
	if len(keep) == 0 {
		return nil, nil
	}
	str := func(k string) string { s, _ := m[k].(string); return s }
	return str("$prefix") + strings.Join(keep, str("$sep")) + str("$suffix"), nil
}
