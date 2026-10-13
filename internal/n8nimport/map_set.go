package n8nimport

import (
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	Register("n8n-nodes-base.set", mapSet)
}

// assignment is one field an n8n Set node writes.
type assignment struct {
	name  string
	value interface{} // raw n8n value (may be an "=…" expression)
	typ   string      // mono core.set type: string, number, bool, json
}

// setAssignments reads the fields of every Set version.
func setAssignments(c *MapContext) []assignment {
	var out []assignment
	switch {
	case c.version(3.3):
		for _, a := range c.paramList("assignments.assignments") {
			out = append(out, assignment{name: fmt.Sprint(a["name"]), value: a["value"], typ: setType(fmt.Sprint(a["type"]))})
		}
	case c.version(3):
		for _, f := range c.paramList("fields.values") {
			t, _ := f["type"].(string)
			if t == "" {
				t = "stringValue"
			}
			out = append(out, assignment{name: fmt.Sprint(f["name"]), value: f[t], typ: setType(strings.TrimSuffix(t, "Value"))})
		}
	default:
		values := c.paramMap("values")
		for _, kind := range []string{"string", "number", "boolean"} {
			list, _ := values[kind].([]interface{})
			for _, raw := range list {
				f, _ := raw.(map[string]interface{})
				if f == nil {
					continue
				}
				v := f["value"]
				if v == nil {
					v = map[string]interface{}{"string": "", "number": 0.0, "boolean": false}[kind]
				}
				out = append(out, assignment{name: fmt.Sprint(f["name"]), value: v, typ: setType(kind)})
			}
		}
	}
	return out
}

func setType(n8n string) string {
	switch n8n {
	case "number":
		return "number"
	case "boolean":
		return "bool"
	case "array", "object":
		return "json"
	}
	return "string"
}

// setInclude returns which input fields stay on the item: "all", "none",
// "selected" or "except", with the field list.
func setInclude(c *MapContext) (string, []string) {
	var mode string
	switch {
	case c.version(3.3):
		if !c.Bool("includeOtherFields", false) {
			c.Ignore("include")
			return "none", nil
		}
		mode = c.Literal("include", "all")
	case c.version(3):
		mode = c.Literal("include", "all")
	default:
		if c.Bool("keepOnlySet", false) {
			return "none", nil
		}
		return "all", nil
	}
	var fields string
	switch mode {
	case "selected":
		fields = c.Literal("includeFields", "")
	case "except":
		fields = c.Literal("excludeFields", "")
	}
	var list []string
	for _, f := range strings.Split(fields, ",") {
		if f = strings.TrimSpace(f); f != "" {
			list = append(list, f)
		}
	}
	return mode, list
}

func mapSet(c *MapContext) Mapping {
	c.Ignore("options.includeBinary", "options.stripBinary", "duplicateItem", "duplicateCount")
	if c.Bool("duplicateItem", false) {
		c.Warn("duplicateItem", true, "has no mono-agent equivalent (a test-only n8n option)")
	}
	if c.Has("options.dotNotation") && !c.Bool("options.dotNotation", true) {
		c.Note("field names are always dot paths in mono-agent (n8n dotNotation was off)")
	}
	c.Ignore("options.ignoreConversionErrors")
	include, fields := setInclude(c)
	if c.Literal("mode", "manual") == "raw" {
		return setRaw(c, include, fields)
	}
	assigns := setAssignments(c)
	if m, ok := setViaCode(c, assigns, include, fields); ok {
		return m
	}
	list := make([]interface{}, 0, len(assigns))
	for i, a := range assigns {
		param := fmt.Sprintf("assignments[%d].%s", i, a.name)
		v := c.Translate(param, setValueString(a.value))
		list = append(list, map[string]interface{}{"field": a.name, "value": v, "type": a.typ})
	}
	cfg := map[string]interface{}{"assignments": list, "include_input": include != "none"}
	m := Mapping{Type: "core.set", Config: cfg}
	return withFieldFilter(m, include, fields, assignNames(assigns))
}

// setValueString renders a raw assignment value for core.set, which
// evaluates every value as template text.
func setValueString(v interface{}) interface{} {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64, bool:
		return fmt.Sprint(x)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

func assignNames(a []assignment) []string {
	out := make([]string, len(a))
	for i, x := range a {
		out[i] = x.name
	}
	return out
}

// withFieldFilter appends a core.code step that keeps the assigned fields
// plus the selected input fields ("selected"), or drops the listed input
// fields ("except").
func withFieldFilter(m Mapping, include string, fields, assigned []string) Mapping {
	var body string
	switch include {
	case "selected":
		keep := append(append([]string{}, assigned...), fields...)
		body = "var keep = " + jsValue(keep) + ";\nreturn items.map(function (it) { var o = {}; keep.forEach(function (k) { if (k in it.json) { o[k] = it.json[k]; } }); return o; });"
	case "except":
		body = "var drop = " + jsValue(fields) + ";\nreturn items.map(function (it) { var o = Object.assign({}, it.json); drop.forEach(function (k) { delete o[k]; }); return o; });"
	default:
		return m
	}
	m.Chain = append(m.Chain, ChainStep{Type: "core.code", Config: codeConfig(wrapCode(body))})
	m.EntrySuffix = " (set)"
	return m
}

// setRaw maps mode "raw" (a JSON object, possibly with expressions): the
// JSON is rendered into a helper field by core.set, then spread into the
// item by core.code, which also applies the include mode.
func setRaw(c *MapContext, include string, fields []string) Mapping {
	raw := c.Raw("jsonOutput")
	text, _ := raw.(string)
	if text == "" {
		b, _ := json.Marshal(raw)
		text = string(b)
	}
	value := c.Translate("jsonOutput", text)
	cfg := map[string]interface{}{
		"assignments":   []interface{}{map[string]interface{}{"field": "__n8n_raw", "value": value, "type": "json"}},
		"include_input": true,
	}
	body := "return items.map(function (it) {\n  var r = it.json.__n8n_raw || {};\n  var o = " + jsBase(include, fields, "it.json") +
		";\n  delete o.__n8n_raw;\n  return Object.assign(o, r);\n});"
	return Mapping{Type: "core.set", Config: cfg, EntrySuffix: " (render JSON)",
		Chain: []ChainStep{{Type: "core.code", Config: codeConfig(wrapCode(body))}}}
}

// jsBase is the JavaScript for the starting object of an output item:
// all, none, selected or all-except input fields of src.
func jsBase(include string, fields []string, src string) string {
	switch include {
	case "none":
		return "{}"
	case "selected":
		return "(function (s) { var o = {}; " + jsValue(fields) + ".forEach(function (k) { if (k in s) { o[k] = s[k]; } }); return o; })(" + src + ")"
	case "except":
		return "(function (s) { var o = Object.assign({}, s); " + jsValue(fields) + ".forEach(function (k) { delete o[k]; }); return o; })(" + src + ")"
	}
	return "Object.assign({}, " + src + ")"
}
