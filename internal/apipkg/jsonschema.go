package apipkg

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/monoes/mono-agent/data"
)

// schemaValidator checks a JSON document against the subset of JSON Schema
// apipkg.v1.schema.json uses: $ref (#/$defs/…), type, enum, properties,
// required, additionalProperties, items, pattern, minLength, minItems.
type schemaValidator struct {
	root map[string]any
	defs map[string]any
}

var (
	pkgSchemaOnce sync.Once
	pkgSchema     *schemaValidator
	pkgSchemaErr  error
)

// packageSchema loads data/schemas/apipkg.v1.schema.json.
func packageSchema() (*schemaValidator, error) {
	pkgSchemaOnce.Do(func() {
		b, err := data.SchemasFS.ReadFile("schemas/apipkg.v1.schema.json")
		if err != nil {
			pkgSchemaErr = fmt.Errorf("apipkg: read schema: %w", err)
			return
		}
		var root map[string]any
		if err := json.Unmarshal(b, &root); err != nil {
			pkgSchemaErr = fmt.Errorf("apipkg: parse schema: %w", err)
			return
		}
		defs, _ := root["$defs"].(map[string]any)
		pkgSchema = &schemaValidator{root: root, defs: defs}
	})
	return pkgSchema, pkgSchemaErr
}

// validate returns one message per violation, sorted.
func (v *schemaValidator) validate(doc any) []string {
	var errs []string
	v.check(v.root, doc, "$", &errs)
	sort.Strings(errs)
	return errs
}

func (v *schemaValidator) check(s map[string]any, val any, path string, errs *[]string) {
	if ref, ok := s["$ref"].(string); ok {
		target, _ := v.defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		if target == nil {
			*errs = append(*errs, path+": unresolved "+ref)
			return
		}
		v.check(target, val, path, errs)
		return
	}
	if t, ok := s["type"].(string); ok && !typeMatches(t, val) {
		*errs = append(*errs, fmt.Sprintf("%s: must be %s, got %s", path, article(t), jsonKind(val)))
		return
	}
	if enum, ok := s["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if e == val {
				found = true
			}
		}
		if !found {
			opts := make([]string, len(enum))
			for i, e := range enum {
				opts[i] = fmt.Sprint(e)
			}
			*errs = append(*errs, fmt.Sprintf("%s: %s is not one of: %s", path, jsonText(val), strings.Join(opts, ", ")))
		}
	}
	switch t := val.(type) {
	case string:
		if p, ok := s["pattern"].(string); ok && !regexp.MustCompile(p).MatchString(t) {
			*errs = append(*errs, fmt.Sprintf("%s: %q does not match %s", path, t, p))
		}
		if n, ok := s["minLength"].(float64); ok && float64(len(t)) < n {
			*errs = append(*errs, fmt.Sprintf("%s: must not be empty", path))
		}
	case []any:
		if n, ok := s["minItems"].(float64); ok && float64(len(t)) < n {
			*errs = append(*errs, fmt.Sprintf("%s: needs at least %v item(s)", path, n))
		}
		if items, ok := s["items"].(map[string]any); ok {
			for i, el := range t {
				v.check(items, el, fmt.Sprintf("%s[%d]", path, i), errs)
			}
		}
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if _, ok := t[r.(string)]; !ok {
					*errs = append(*errs, fmt.Sprintf("%s: missing required key %q", path, r))
				}
			}
		}
		for k, el := range t {
			if ps, ok := props[k].(map[string]any); ok {
				v.check(ps, el, path+"."+k, errs)
				continue
			}
			switch ap := s["additionalProperties"].(type) {
			case bool:
				if !ap {
					*errs = append(*errs, fmt.Sprintf("%s: unknown key %q (allowed: %s)", path, k, keyList(props)))
				}
			case map[string]any:
				v.check(ap, el, path+"."+k, errs)
			}
		}
	}
}

func jsonText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func keyList(props map[string]any) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func typeMatches(t string, val any) bool {
	switch t {
	case "object":
		_, ok := val.(map[string]any)
		return ok
	case "array":
		_, ok := val.([]any)
		return ok
	case "string":
		_, ok := val.(string)
		return ok
	case "boolean":
		_, ok := val.(bool)
		return ok
	case "number":
		_, ok := val.(float64)
		return ok
	case "integer":
		f, ok := val.(float64)
		return ok && f == math.Trunc(f)
	}
	return true
}

func jsonKind(val any) string {
	switch val.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case float64:
		return "a number"
	}
	return fmt.Sprintf("%T", val)
}

func article(t string) string {
	if t == "object" || t == "array" || t == "integer" {
		return "an " + t
	}
	return "a " + t
}
