package httpnodes

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// nameValue is one header, query parameter or form field. Value is a
// scalar, or a []interface{} for a repeated query parameter.
type nameValue struct {
	Name  string
	Value interface{}
}

// configString reads a string config value. Numbers and booleans (a
// templated value can arrive as either) are formatted; anything else is "".
func configString(config map[string]interface{}, key string) string {
	switch v := config[key].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// configNumber reads a numeric config value: a JSON number, a Go int, or a
// numeric string (expressions resolve to strings). ok is false when the key
// is absent or empty; err names the field when the value is not a number.
func configNumber(config map[string]interface{}, key string) (val float64, ok bool, err error) {
	switch v := config[key].(type) {
	case nil:
		return 0, false, nil
	case float64:
		return v, true, nil
	case int:
		return float64(v), true, nil
	case int64:
		return float64(v), true, nil
	case json.Number:
		f, perr := v.Float64()
		if perr != nil {
			return 0, false, fmt.Errorf("%s: %q is not a number", key, v.String())
		}
		return f, true, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, false, nil
		}
		f, perr := strconv.ParseFloat(s, 64)
		if perr != nil {
			return 0, false, fmt.Errorf("%s: %q is not a number", key, v)
		}
		return f, true, nil
	default:
		return 0, false, fmt.Errorf("%s: expected a number, got %T", key, v)
	}
}

// configBool reads a boolean config value (a bool or "true"/"false").
func configBool(config map[string]interface{}, key string, def bool) (bool, error) {
	switch v := config[key].(type) {
	case nil:
		return def, nil
	case bool:
		return v, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return def, nil
		}
		b, err := strconv.ParseBool(s)
		if err != nil {
			return def, fmt.Errorf("%s: %q is not true or false", key, v)
		}
		return b, nil
	default:
		return def, fmt.Errorf("%s: expected true or false, got %T", key, v)
	}
}

// oneOf normalises a select-style config value: empty gives def, aliases
// map other spellings (e.g. n8n's) to ours, and anything not in allowed is
// an error that names the field and the accepted values.
func oneOf(config map[string]interface{}, key, def string, allowed []string, aliases map[string]string) (string, error) {
	v := strings.TrimSpace(configString(config, key))
	if v == "" {
		return def, nil
	}
	if a, ok := aliases[v]; ok {
		v = a
	}
	lv := strings.ToLower(v)
	for _, a := range allowed {
		if lv == a {
			return a, nil
		}
	}
	return "", fmt.Errorf("%s: unknown value %q (use one of: %s)", key, v, strings.Join(allowed, ", "))
}

// parseNameValues reads a headers / query_params / form-body config value.
// Accepted shapes:
//   - an object: {"name": "value", ...} (a list value repeats the name)
//   - a list of {"name": ..., "value": ...} objects (n8n's key/value rows)
//   - a string holding either of the above as JSON
//
// Object keys come back sorted so requests are deterministic.
func parseNameValues(v interface{}, field string) ([]nameValue, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]nameValue, 0, len(keys))
		for _, k := range keys {
			out = append(out, nameValue{Name: k, Value: t[k]})
		}
		return out, nil
	case map[string]string:
		m := make(map[string]interface{}, len(t))
		for k, s := range t {
			m[k] = s
		}
		return parseNameValues(m, field)
	case []interface{}:
		out := make([]nameValue, 0, len(t))
		for i, raw := range t {
			row, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("%s[%d]: expected an object with name and value, got %T", field, i, raw)
			}
			name, _ := row["name"].(string)
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("%s[%d]: name is empty", field, i)
			}
			out = append(out, nameValue{Name: name, Value: row["value"]})
		}
		return out, nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil, nil
		}
		var decoded interface{}
		if err := json.Unmarshal([]byte(s), &decoded); err != nil {
			return nil, fmt.Errorf("%s: not valid JSON (want an object like {\"name\": \"value\"}): %v", field, err)
		}
		switch decoded.(type) {
		case map[string]interface{}, []interface{}:
			return parseNameValues(decoded, field)
		}
		return nil, fmt.Errorf("%s: want a JSON object or a list of {name, value}, got %T", field, decoded)
	default:
		return nil, fmt.Errorf("%s: want an object or a list of {name, value}, got %T", field, v)
	}
}

// stringifyValue renders a header/query/form value: scalars as text,
// objects and lists as JSON.
func stringifyValue(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case map[string]interface{}, []interface{}:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// applyQuery merges params into q. A name given here replaces the same
// name already in the URL; a list value repeats the name in the style of
// arrayFormat: "repeat" (a=1&a=2), "brackets" (a[]=1&a[]=2) or "indices"
// (a[0]=1&a[1]=2). skip names keys that must be left as they are.
func applyQuery(q url.Values, params []nameValue, arrayFormat string, skip map[string]bool) {
	reset := map[string]bool{}
	add := func(name, value string) {
		if !reset[name] {
			q.Del(name)
			reset[name] = true
		}
		q.Add(name, value)
	}
	for _, p := range params {
		if skip[p.Name] {
			continue
		}
		list, isList := p.Value.([]interface{})
		if !isList {
			add(p.Name, stringifyValue(p.Value))
			continue
		}
		for i, el := range list {
			switch arrayFormat {
			case "brackets":
				add(p.Name+"[]", stringifyValue(el))
			case "indices":
				add(fmt.Sprintf("%s[%d]", p.Name, i), stringifyValue(el))
			default:
				add(p.Name, stringifyValue(el))
			}
		}
	}
}
