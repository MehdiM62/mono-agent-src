package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

// parseOutputSchema reads the output_schema config: a JSON Schema object, or
// a JSON string holding one. nil, nil when it is not set.
func parseOutputSchema(v interface{}) (map[string]interface{}, error) {
	switch s := v.(type) {
	case nil:
		return nil, nil
	case map[string]interface{}:
		if len(s) == 0 {
			return nil, nil
		}
		return s, nil
	case string:
		if strings.TrimSpace(s) == "" {
			return nil, nil
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			return nil, fmt.Errorf("output_schema is not a JSON object: %w", err)
		}
		return m, nil
	}
	return nil, fmt.Errorf("output_schema must be a JSON Schema object or a JSON string, got %T", v)
}

// schemaInstruction is what is appended to the prompt when output_schema is set.
func schemaInstruction(schema map[string]interface{}) string {
	b, _ := json.Marshal(schema)
	return "\n\nReply with only a JSON value that matches this JSON Schema, and nothing else (no prose, no code fences):\n" + string(b)
}

// extractJSON finds the JSON value in a reply: the whole reply, else the
// body of a ```json fence, else the first {...} or [...] that parses.
func extractJSON(reply string) (interface{}, error) {
	s := strings.TrimSpace(reply)
	if v, ok := parseJSON(s); ok {
		return v, nil
	}
	if i := strings.Index(s, "```"); i >= 0 {
		body := s[i+3:]
		if nl := strings.IndexByte(body, '\n'); nl >= 0 && !strings.ContainsAny(body[:nl], "{[") {
			body = body[nl+1:] // drop the language tag line
		}
		if j := strings.Index(body, "```"); j >= 0 {
			body = body[:j]
		}
		if v, ok := parseJSON(strings.TrimSpace(body)); ok {
			return v, nil
		}
	}
	for i, r := range s {
		if r != '{' && r != '[' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(s[i:]))
		dec.UseNumber()
		var v interface{}
		if err := dec.Decode(&v); err == nil {
			return normalizeNumbers(v), nil
		}
	}
	return nil, fmt.Errorf("the reply has no JSON value")
}

func parseJSON(s string) (interface{}, bool) {
	if s == "" {
		return nil, false
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil || dec.More() {
		return nil, false
	}
	return normalizeNumbers(v), true
}

// normalizeNumbers turns json.Number into float64, as encoding/json does by default.
func normalizeNumbers(v interface{}) interface{} {
	switch t := v.(type) {
	case json.Number:
		f, _ := t.Float64()
		return f
	case map[string]interface{}:
		for k, e := range t {
			t[k] = normalizeNumbers(e)
		}
	case []interface{}:
		for i, e := range t {
			t[i] = normalizeNumbers(e)
		}
	}
	return v
}

// validateSchema checks v against a pragmatic subset of JSON Schema: type
// (a name or a list), required, properties, items, enum and const. Other
// keywords are ignored.
func validateSchema(schema map[string]interface{}, v interface{}) error {
	return validateAt("$", schema, v)
}

func validateAt(path string, schema map[string]interface{}, v interface{}) error {
	if t, ok := schema["type"]; ok {
		var types []string
		switch tt := t.(type) {
		case string:
			types = []string{tt}
		case []interface{}:
			for _, e := range tt {
				if s, ok := e.(string); ok {
					types = append(types, s)
				}
			}
		}
		if len(types) > 0 && !matchesAnyType(types, v) {
			return fmt.Errorf("%s: want %s, got %s", path, strings.Join(types, " or "), jsonType(v))
		}
	}
	if enum, ok := schema["enum"].([]interface{}); ok {
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
	if obj, ok := v.(map[string]interface{}); ok {
		if req, ok := schema["required"].([]interface{}); ok {
			for _, r := range req {
				if k, ok := r.(string); ok {
					if _, has := obj[k]; !has {
						return fmt.Errorf("%s: missing required property %q", path, k)
					}
				}
			}
		}
		if props, ok := schema["properties"].(map[string]interface{}); ok {
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sub, ok := props[k].(map[string]interface{})
				val, has := obj[k]
				if !ok || !has {
					continue
				}
				if err := validateAt(path+"."+k, sub, val); err != nil {
					return err
				}
			}
		}
	}
	if arr, ok := v.([]interface{}); ok {
		if items, ok := schema["items"].(map[string]interface{}); ok {
			for i, e := range arr {
				if err := validateAt(fmt.Sprintf("%s[%d]", path, i), items, e); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func matchesAnyType(types []string, v interface{}) bool {
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

func jsonType(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}
