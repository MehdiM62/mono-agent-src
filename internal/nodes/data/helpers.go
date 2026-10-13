package data

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Typed accessors for the map[string]interface{} config every node receives.
// Config arrives either from workflow JSON (numbers are float64) or from Go
// callers and tests (numbers may be int), and list-shaped options may be a
// real []interface{} or a JSON string typed into a textarea — the helpers
// below accept all of those shapes so each node parses its config one way.

func cfgString(config map[string]interface{}, key, def string) string {
	switch v := config[key].(type) {
	case string:
		if v != "" {
			return v
		}
	case nil:
	default:
		return fmt.Sprintf("%v", v)
	}
	return def
}

func cfgBool(config map[string]interface{}, key string, def bool) bool {
	switch v := config[key].(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}

func cfgFloat(config map[string]interface{}, key string, def float64) (float64, error) {
	v, ok := config[key]
	if !ok || v == nil {
		return def, nil
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
		return def, nil
	}
	f, ok := toFloat(v)
	if !ok {
		return 0, fmt.Errorf("%s: %v is not a number", key, v)
	}
	return f, nil
}

func cfgInt(config map[string]interface{}, key string, def int) (int, error) {
	f, err := cfgFloat(config, key, float64(def))
	if err != nil {
		return 0, err
	}
	return int(f), nil
}

// cfgList returns a list-shaped config value: a []interface{} as-is, a JSON
// array string decoded, an empty value as nil.
func cfgList(config map[string]interface{}, key string) ([]interface{}, error) {
	switch v := config[key].(type) {
	case nil:
		return nil, nil
	case []interface{}:
		return v, nil
	case []map[string]interface{}:
		out := make([]interface{}, len(v))
		for i, m := range v {
			out[i] = m
		}
		return out, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		var out []interface{}
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("%s: expected a JSON array: %w", key, err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s: expected a list, got %T", key, v)
	}
}

// cfgObject returns an object-shaped config value from a map or a JSON
// object string.
func cfgObject(config map[string]interface{}, key string) (map[string]interface{}, error) {
	switch v := config[key].(type) {
	case nil:
		return nil, nil
	case map[string]interface{}:
		return v, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("%s: expected a JSON object: %w", key, err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s: expected an object, got %T", key, v)
	}
}

// cfgNames returns a list of names given either as a comma-separated string
// or as a list of strings, with blanks dropped.
func cfgNames(config map[string]interface{}, key string) []string {
	var raw []string
	switch v := config[key].(type) {
	case string:
		raw = strings.Split(v, ",")
	case []interface{}:
		for _, e := range v {
			raw = append(raw, fmt.Sprintf("%v", e))
		}
	case []string:
		raw = v
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil && !math.IsNaN(f)
	}
	return 0, false
}

// ── dot-path access ───────────────────────────────────────────────────────────

// splitPath turns "a.b[0].c" or "a.b.0.c" into its segments. Bracketed
// segments may be quoted (a["x.y"]) so keys containing dots stay reachable.
func splitPath(path string) []string {
	var segs []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			segs = append(segs, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(path); i++ {
		c := path[i]
		switch c {
		case '.':
			flush()
		case '[':
			flush()
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				cur.WriteString(path[i:])
				i = len(path)
				continue
			}
			inner := strings.Trim(path[i+1:i+end], `"'`)
			segs = append(segs, inner)
			i += end
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return segs
}

// getPath reads a value by dot path. With literal=true the path is used as
// one key (n8n's "disable dot notation").
func getPath(m map[string]interface{}, path string, literal bool) (interface{}, bool) {
	if literal {
		v, ok := m[path]
		return v, ok
	}
	if v, ok := m[path]; ok {
		return v, true
	}
	var cur interface{} = m
	for _, seg := range splitPath(path) {
		switch node := cur.(type) {
		case map[string]interface{}:
			v, ok := node[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []interface{}:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// setPath writes a value by dot path, creating intermediate objects.
func setPath(m map[string]interface{}, path string, val interface{}, literal bool) {
	if literal {
		m[path] = val
		return
	}
	segs := splitPath(path)
	if len(segs) == 0 {
		return
	}
	cur := m
	for _, seg := range segs[:len(segs)-1] {
		next, ok := cur[seg].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			cur[seg] = next
		}
		cur = next
	}
	cur[segs[len(segs)-1]] = val
}

// unsetPath deletes a value by dot path; missing paths are a no-op.
func unsetPath(m map[string]interface{}, path string, literal bool) {
	if literal {
		delete(m, path)
		return
	}
	if _, ok := m[path]; ok {
		delete(m, path)
		return
	}
	segs := splitPath(path)
	if len(segs) == 0 {
		return
	}
	cur := m
	for _, seg := range segs[:len(segs)-1] {
		next, ok := cur[seg].(map[string]interface{})
		if !ok {
			return
		}
		cur = next
	}
	delete(cur, segs[len(segs)-1])
}

// deepCopyValue copies maps and slices recursively so nested edits never
// reach the input item.
func deepCopyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, e := range t {
			out[k] = deepCopyValue(e)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = deepCopyValue(e)
		}
		return out
	default:
		return v
	}
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	return deepCopyValue(m).(map[string]interface{})
}

// isEmptyValue mirrors the "empty" notion of n8n's transform nodes: nil or "".
func isEmptyValue(v interface{}) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}
