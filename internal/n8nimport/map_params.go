package n8nimport

import (
	"sort"
	"strconv"
	"strings"
)

// paramSpec converts an n8n node through the parameter tables the target
// node's package publishes (internal/nodes/data, internal/nodes/image, …):
// each set n8n parameter is copied, expressions translated, under our
// config key. The tables are owned next to the nodes, so a node that grows
// an option only needs its table updated.
type paramSpec struct {
	Type string
	// Params maps an n8n parameter path ("options.timezone",
	// "sortFieldsUi.sortField[0].fieldName") to our config key. "" drops
	// the parameter on purpose. A dotted key ("claims.audience") is set
	// inside an object.
	Params map[string]string
	// Values rewrites option values per our key: our key → n8n → ours.
	Values map[string]map[string]string
	// ListFields renames the fields of each element of a list value (n8n
	// element field, possibly dotted → our flat field), keeping the rest.
	ListFields map[string]map[string]string
	// Defaults are n8n parameter values n8n leaves out of exports when they
	// hold their default; applied (then mapped) when the parameter is absent.
	Defaults map[string]interface{}
	// Fixed is config every converted node gets.
	Fixed map[string]interface{}
}

// mapWithParams applies spec to the node. Parameters the tables do not
// name are left for reportUnused to list.
func mapWithParams(c *MapContext, spec paramSpec) Mapping {
	cfg := map[string]interface{}{}
	for k, v := range spec.Fixed {
		cfg[k] = v
	}
	paths := make([]string, 0, len(spec.Params))
	for p := range spec.Params {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		key := spec.Params[path]
		raw, ok := lookupIndexed(c.Params, path)
		if !ok {
			def, hasDef := spec.Defaults[path]
			if !hasDef || key == "" {
				continue
			}
			raw = def
		} else {
			c.markUsed(path)
			if key == "" {
				continue
			}
			if m, isMap := raw.(map[string]interface{}); isMap && hasChildPaths(spec.Params, path) {
				// The children are mapped one by one; keep only the rest.
				rest := map[string]interface{}{}
				for k, v := range m {
					if _, mapped := spec.Params[path+"."+k]; !mapped {
						rest[k] = v
					}
				}
				if len(rest) == 0 {
					continue
				}
				raw = rest
			}
		}
		v := c.Translate(path, raw)
		if s, isStr := v.(string); isStr {
			if to, ok := spec.Values[key][s]; ok {
				v = to
			}
		}
		if fields, ok := spec.ListFields[key]; ok {
			v = renameListFields(v, fields)
		}
		setDotted(cfg, key, v)
	}
	return Mapping{Type: spec.Type, Config: cfg}
}

func hasChildPaths(params map[string]string, path string) bool {
	for p := range params {
		if strings.HasPrefix(p, path+".") {
			return true
		}
	}
	return false
}

// lookupIndexed is lookupPath with "name[i]" list indexes.
func lookupIndexed(m map[string]interface{}, path string) (interface{}, bool) {
	cur := interface{}(m)
	for _, part := range strings.Split(path, ".") {
		idx := -1
		if open := strings.IndexByte(part, '['); open > 0 && strings.HasSuffix(part, "]") {
			n, err := strconv.Atoi(part[open+1 : len(part)-1])
			if err != nil {
				return nil, false
			}
			part, idx = part[:open], n
		}
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		if cur, ok = mm[part]; !ok {
			return nil, false
		}
		if idx >= 0 {
			list, ok := cur.([]interface{})
			if !ok || idx >= len(list) {
				return nil, false
			}
			cur = list[idx]
		}
	}
	return cur, true
}

// setDotted sets key in cfg, creating objects for a dotted key.
func setDotted(cfg map[string]interface{}, key string, v interface{}) {
	parts := strings.Split(key, ".")
	cur := cfg
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			cur[p] = next
		}
		cur = next
	}
	last := parts[len(parts)-1]
	if _, set := cur[last]; set {
		return // the first (shortest) n8n path wins
	}
	cur[last] = v
}

// renameListFields renames the fields of each object in a list.
func renameListFields(v interface{}, fields map[string]string) interface{} {
	list, ok := v.([]interface{})
	if !ok {
		return v
	}
	out := make([]interface{}, len(list))
	for i, el := range list {
		m, ok := el.(map[string]interface{})
		if !ok {
			out[i] = el
			continue
		}
		o := map[string]interface{}{}
		for k, x := range m {
			o[k] = x
		}
		for from, to := range fields {
			x, ok := lookupPath(m, from)
			if !ok {
				continue
			}
			parts := strings.Split(from, ".")
			if len(parts) > 1 {
				if parent, ok := o[parts[0]].(map[string]interface{}); ok {
					cp := map[string]interface{}{}
					for k, y := range parent {
						if k != parts[1] {
							cp[k] = y
						}
					}
					if len(cp) == 0 {
						delete(o, parts[0])
					} else {
						o[parts[0]] = cp
					}
				}
			} else {
				delete(o, from)
			}
			o[to] = x
		}
		out[i] = o
	}
	return out
}
