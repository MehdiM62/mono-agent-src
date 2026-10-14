package apipkg

import (
	"encoding/json"
	"fmt"
	"github.com/monoes/mono-agent/internal/workflow"
)

func checkTemplates(where string, v any, fields, credKeys map[string]bool) []string {
	var probs []string
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			if !hasTemplate(t) {
				return
			}
			tm, err := parseTemplate(t)
			if err != nil {
				probs = append(probs, fmt.Sprintf("%s: %v", where, err))
				return
			}
			for _, part := range tm.parts {
				if part.expr == nil {
					continue
				}
				exprs := []*expr{part.expr}
				for h := part.expr.head; h != nil; h = h.head {
					exprs = append(exprs, h)
				}
				for _, f := range part.expr.filters {
					for _, a := range f.args {
						if a != f.arg && a.path != nil {
							exprs = append(exprs, a.path)
						}
						if a.sub != nil {
							exprs = append(exprs, a.sub)
						}
					}
					if f.arg != nil && f.arg.sub != nil && len(f.args) == 0 {
						exprs = append(exprs, f.arg.sub)
					}
					if f.arg != nil && f.arg.path != nil {
						exprs = append(exprs, f.arg.path)
					}
				}
				for _, e := range exprs {
					probs = append(probs, checkRef(where, e, fields, credKeys)...)
				}
			}
		case map[string]any:
			for k, el := range t {
				walk(k)
				walk(el)
			}
		case []any:
			for _, el := range t {
				walk(el)
			}
		}
	}
	walk(v)
	return probs
}

// anyField marks templates (credential inject) that may read any op
// param: they are shared by every op, so p.* can't be checked per op.
var anyField = map[string]bool{"*": true}

func checkRef(where string, e *expr, fields, credKeys map[string]bool) []string {
	if len(e.path) == 0 {
		return nil
	}
	switch e.root {
	case "p":
		if fields == nil {
			return []string{fmt.Sprintf("%s: {{ %s }}: p.* is not available here (only cred.*)", where, e.src)}
		}
		if !fields[e.path[0]] && !fields["*"] {
			return []string{fmt.Sprintf("%s: {{ %s }}: %q is not one of the op's fields", where, e.src, e.path[0])}
		}
	case "req":
		return nil
	case "cred":
		if !credKeys[e.path[0]] {
			return []string{fmt.Sprintf("%s: {{ %s }}: %q is not a credential field", where, e.src, e.path[0])}
		}
	case "hook":
		if !fields[hookFieldsMarker] {
			return []string{fmt.Sprintf("%s: {{ %s }}: hook.* is only available in the package's webhook block", where, e.src)}
		}
	case "item", "el", "up", "res", "items", "pre", "pres", "bin", "main", "hdr", "idx", "page", "batch":
		if fields == nil {
			return []string{fmt.Sprintf("%s: {{ %s }}: %s.* is not available here (only cred.*)", where, e.src, e.root)}
		}
	}
	return nil
}

// nativeOperations lists the "operation" select options of a native node's
// own schema file (without any extension merged in).
func nativeOperations(nodeType string) map[string]bool {
	out := map[string]bool{}
	b, ok := workflow.ReadSchemaFile(nodeType)
	if !ok {
		return out
	}
	var doc struct {
		Fields []struct {
			Key     string   `json:"key"`
			Options []string `json:"options"`
		} `json:"fields"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return out
	}
	for _, f := range doc.Fields {
		if f.Key == "operation" {
			for _, o := range f.Options {
				out[o] = true
			}
		}
	}
	return out
}

// storedKeys are the keys an OAuth connect stores beyond the token itself:
// token_fields and the metadata fields.
func (o *OAuthSpec) storedKeys() []string {
	keys := append([]string(nil), o.TokenFields...)
	if o.Metadata != nil {
		for k := range o.Metadata.Fields {
			keys = append(keys, k)
		}
	}
	return keys
}
