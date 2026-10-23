package aitask

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

// Validate checks v (decoded with encoding/json) against schema, a pragmatic
// subset of JSON Schema: type (a name or a list), required, properties,
// additionalProperties:false, items, enum, const, minLength, maxLength and minItems.
// Other keywords are ignored. The error names the first problem by its path,
// so the agent that wrote the result can fix it. It is the same subset
// internal/nodes/agent checks; that one is unexported, and the task result
// needs it without importing a node package.
func Validate(schema map[string]any, v any) error {
	return validateAt("$", schema, v)
}

func validateAt(path string, schema map[string]any, v any) error {
	if types := typeList(schema["type"]); len(types) > 0 && !matchesAnyType(types, v) {
		return fmt.Errorf("%s: want %s, got %s", path, strings.Join(types, " or "), jsonType(v))
	}
	if enum, ok := schema["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if reflect.DeepEqual(e, v) {
				found = true
				break
			}
		}
		if !found {
			b, _ := json.Marshal(enum)
			return fmt.Errorf("%s: must be one of %s", path, b)
		}
	}
	if c, ok := schema["const"]; ok && !reflect.DeepEqual(c, v) {
		b, _ := json.Marshal(c)
		return fmt.Errorf("%s: must be %s", path, b)
	}
	switch t := v.(type) {
	case string:
		if n, ok := schema["minLength"].(float64); ok && float64(len([]rune(t))) < n {
			return fmt.Errorf("%s: must be at least %d characters", path, int(n))
		}
		if n, ok := schema["maxLength"].(float64); ok && float64(utf8.RuneCountInString(t)) > n {
			return fmt.Errorf("%s: must be at most %d characters", path, int(n))
		}
	case map[string]any:
		return validateObject(path, schema, t)
	case []any:
		if n, ok := schema["minItems"].(float64); ok && float64(len(t)) < n {
			return fmt.Errorf("%s: must have at least %d items", path, int(n))
		}
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range t {
				if err := validateAt(fmt.Sprintf("%s[%d]", path, i), items, e); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateObject(path string, schema map[string]any, obj map[string]any) error {
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if k, ok := r.(string); ok {
				if _, has := obj[k]; !has {
					return fmt.Errorf("%s: missing required property %q", path, k)
				}
			}
		}
	}
	props, _ := schema["properties"].(map[string]any)
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sub, ok := props[k].(map[string]any)
		val, has := obj[k]
		if !ok || !has {
			continue
		}
		if err := validateAt(path+"."+k, sub, val); err != nil {
			return err
		}
	}
	if ap, ok := schema["additionalProperties"].(bool); ok && !ap {
		var extra []string
		for k := range obj {
			if _, known := props[k]; !known {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			return fmt.Errorf("%s: unexpected property %q (allowed: %s)", path, extra[0], strings.Join(keys, ", "))
		}
	}
	return nil
}

func typeList(t any) []string {
	switch tt := t.(type) {
	case string:
		return []string{tt}
	case []any:
		var out []string
		for _, e := range tt {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func matchesAnyType(types []string, v any) bool {
	for _, t := range types {
		switch t {
		case "integer":
			if f, ok := v.(float64); ok && f == math.Trunc(f) {
				return true
			}
		case "number":
			if _, ok := v.(float64); ok {
				return true
			}
		default:
			if jsonType(v) == t {
				return true
			}
		}
	}
	return false
}

func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}
