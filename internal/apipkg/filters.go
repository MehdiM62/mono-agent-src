package apipkg

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var knownFilters = map[string]bool{
	"json": true, "csv": true, "int": true, "float": true, "bool": true,
	"string": true, "lower": true, "upper": true, "default": true, "rl": true,
	"then": true, "eq": true, "ne": true, "kv": true, "omit": true,
}

// applyFilter runs one pipe filter. Filters other than default and then pass
// nil through, so an unset param stays unset (and is omitted).
func applyFilter(name string, v, arg any) (any, error) {
	switch name {
	case "default":
		if isEmpty(v) {
			return arg, nil
		}
		return v, nil
	case "then":
		if isEmpty(v) {
			return nil, nil
		}
		return arg, nil
	case "eq":
		if stringify(v) == stringify(arg) {
			return true, nil
		}
		return nil, nil
	case "ne":
		if stringify(v) != stringify(arg) {
			return true, nil
		}
		return nil, nil
	case "rl":
		// n8n resource locator: {"__rl": true, "mode": "id", "value": "x"}.
		if m, ok := v.(map[string]any); ok {
			if val, ok := m["value"]; ok {
				return val, nil
			}
		}
		return v, nil
	}
	if v == nil {
		return nil, nil
	}
	switch name {
	case "json":
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("json: %w", err)
		}
		return string(b), nil
	case "csv":
		return toCSV(v), nil
	case "int":
		return toInt(v)
	case "float":
		return toFloat(v)
	case "bool":
		return toBool(v)
	case "string":
		return stringify(v), nil
	case "kv":
		return toKV(v, arg)
	case "omit":
		return omitKeys(v, arg)
	case "lower":
		return strings.ToLower(stringify(v)), nil
	case "upper":
		return strings.ToUpper(stringify(v)), nil
	}
	return nil, fmt.Errorf("unknown filter %q", name)
}

func toCSV(v any) any {
	switch t := v.(type) {
	case []any:
		parts := make([]string, 0, len(t))
		for _, el := range t {
			parts = append(parts, stringify(el))
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(t, ",")
	}
	return stringify(v)
}

func toInt(v any) (any, error) {
	switch t := v.(type) {
	case float64:
		return int64(math.Trunc(t)), nil
	case int:
		return int64(t), nil
	case int64:
		return t, nil
	case bool:
		if t {
			return int64(1), nil
		}
		return int64(0), nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil, nil
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("int: %q is not a number", t)
		}
		return int64(math.Trunc(f)), nil
	}
	return nil, fmt.Errorf("int: cannot convert %T", v)
}

func toFloat(v any) (any, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("float: %q is not a number", t)
		}
		return f, nil
	}
	return nil, fmt.Errorf("float: cannot convert %T", v)
}

func toBool(v any) (any, error) {
	switch t := v.(type) {
	case bool:
		return t, nil
	case float64:
		return t != 0, nil
	case int:
		return t != 0, nil
	case int64:
		return t != 0, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "":
			return nil, nil
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		}
		return nil, fmt.Errorf("bool: %q is not a boolean", t)
	}
	return nil, fmt.Errorf("bool: cannot convert %T", v)
}

// toKV turns a list of {<key>: k, <value>: v} rows (n8n's fixedCollection
// key/value lists) into the object {k: v}. arg is "keyField,valueField".
// A single-key object wrapping the list (e.g. {"fieldValues": [...]}) is
// unwrapped first.
func toKV(v, arg any) (any, error) {
	names := strings.SplitN(stringify(arg), ",", 2)
	if len(names) != 2 || strings.TrimSpace(names[0]) == "" || strings.TrimSpace(names[1]) == "" {
		return nil, fmt.Errorf(`kv: argument must be "keyField,valueField", got %q`, stringify(arg))
	}
	kf, vf := strings.TrimSpace(names[0]), strings.TrimSpace(names[1])
	if m, ok := v.(map[string]any); ok && len(m) == 1 {
		for _, inner := range m {
			v = inner
		}
	}
	rows, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("kv: value is %T, not a list of rows", v)
	}
	out := map[string]any{}
	for i, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("kv: row %d is %T, not an object", i, r)
		}
		k := stringify(row[kf])
		if k == "" {
			continue
		}
		out[k] = row[vf]
	}
	return out, nil
}

// omitKeys returns a copy of the object v without the keys named in arg
// (a comma-separated string or a list).
func omitKeys(v, arg any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("omit: value is %T, not an object", v)
	}
	drop := map[string]bool{}
	switch a := arg.(type) {
	case []any:
		for _, x := range a {
			drop[strings.TrimSpace(stringify(x))] = true
		}
	default:
		for _, x := range strings.Split(stringify(a), ",") {
			drop[strings.TrimSpace(x)] = true
		}
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		if !drop[k] {
			out[k] = val
		}
	}
	return out, nil
}
