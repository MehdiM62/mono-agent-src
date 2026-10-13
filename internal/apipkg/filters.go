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
	"then": true,
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
