package n8nimport

import (
	"fmt"
	"strconv"
	"strings"
)

func init() {
	Register("n8n-nodes-base.if", mapIf)
	Register("n8n-nodes-base.filter", mapFilter)
	Register("n8n-nodes-base.switch", mapSwitch)
}

// readConditions reads the conditions of an If or Filter node.
func readConditions(c *MapContext, v2From float64, combineParam string) condSet {
	c.Ignore("looseTypeValidation", "options.looseTypeValidation")
	if c.version(v2From) {
		f := c.paramMap("conditions")
		return conditionsV2(f, c.Bool("options.ignoreCase", true))
	}
	combine := c.Literal(combineParam, "all")
	return conditionsV1(c.paramMap("conditions"), combine == "any" || combine == "OR")
}

// condFailed records a condition the template language cannot express.
func condFailed(c *MapContext, err error) {
	c.exprs = append(c.exprs, ExprIssue{Param: "conditions", Expression: compactValue(c.Params["conditions"]), Reason: err.Error()})
}

func mapIf(c *MapContext) Mapping {
	set := readConditions(c, 2, "combineOperation")
	tmpl, err := c.condTemplate("conditions", set)
	if err == nil {
		return Mapping{Type: "core.if", Outputs: []string{"true", "false"},
			Config: map[string]interface{}{"condition": "{{ " + tmpl + " }}", "mode": "per_item"}}
	}
	if js, ok := jsCondSet(set); ok {
		c.Warn("conditions", nil, "evaluated in a core.code step (%s); items leaving this node carry the helper field __n8n_if", err)
		body := jsValueHelpers + jsCondHelpers + "var o = Object.assign({}, $json);\no.__n8n_if = !!(" + js + ");\nreturn o;\n"
		return Mapping{Type: "core.code", Config: codeConfig(wrapPerItem(body)), EntrySuffix: " (evaluate)",
			Outputs: []string{"true", "false"},
			Chain:   []ChainStep{{Type: "core.if", Config: map[string]interface{}{"condition": "{{ $json.__n8n_if }}", "mode": "per_item"}}}}
	}
	condFailed(c, err)
	return Mapping{Type: "core.if", Outputs: []string{"true", "false"},
		Config: map[string]interface{}{"condition": "{{ false }}", "mode": "per_item"}}
}

func mapFilter(c *MapContext) Mapping {
	set := readConditions(c, 2, "combineConditions")
	tmpl, err := c.condTemplate("conditions", set)
	if err == nil {
		return Mapping{Type: "core.filter", Config: map[string]interface{}{"condition": "{{ " + tmpl + " }}", "mode": "keep"}}
	}
	if js, ok := jsCondSet(set); ok {
		c.Note("conditions run as JavaScript in core.code (%s)", err)
		body := jsValueHelpers + jsCondHelpers + "return (" + js + ") ? $json : null;\n"
		return Mapping{Type: "core.code", Config: codeConfig(wrapPerItem(body))}
	}
	condFailed(c, err)
	return Mapping{Type: "core.filter", Config: map[string]interface{}{"condition": "{{ false }}", "mode": "keep"}}
}

// switchRule is one routing rule: a condition and the output it sends to.
type switchRule struct {
	set    condSet
	output int
}

// switchHandles names output i "0", "1", … so connections keep their index.
func switchHandles(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strconv.Itoa(i)
	}
	return out
}

func mapSwitch(c *MapContext) Mapping {
	c.Ignore("looseTypeValidation", "options.looseTypeValidation", "options.renameFallbackOutput")
	mode := c.Literal("mode", "rules")
	if mode == "expression" {
		return switchExpression(c)
	}
	var rules []switchRule
	outputs := 4
	fallback := -1
	switch {
	case c.version(3):
		ignoreCase := c.Bool("options.ignoreCase", true)
		for i, r := range c.paramList("rules.values") {
			f, _ := r["conditions"].(map[string]interface{})
			rules = append(rules, switchRule{set: conditionsV2(f, ignoreCase), output: i})
		}
		outputs = len(rules)
		switch fb := c.Raw("options.fallbackOutput").(type) {
		case string:
			if fb == "extra" {
				fallback = outputs
				outputs++
			}
		case float64:
			fallback = int(fb)
		}
		if c.Bool("options.allMatchingOutputs", false) {
			c.Warn("options.allMatchingOutputs", true, "mono-agent routes an item to its first matching rule only")
		}
	default:
		dataType := c.Literal("dataType", "number")
		value1 := c.Raw("value1")
		for i, r := range c.paramList("rules.rules") {
			op, _ := r["operation"].(string)
			if op == "" {
				op = "equal"
			}
			out := i
			if !c.version(2) {
				if o, ok := r["output"].(float64); ok {
					out = int(o)
				} else {
					out = 0
				}
			}
			rules = append(rules, switchRule{output: out, set: condSet{caseSensitive: true,
				conds: []cond{{left: value1, right: r["value2"], typ: dataType, op: v2Op(op)}}}})
		}
		if c.version(2) {
			outputs = len(rules)
		}
		fallback = int(c.Float("fallbackOutput", -1))
	}
	return switchMapping(c, rules, outputs, fallback)
}

// switchMapping builds a core.switch whose field expression yields the
// index of the first matching rule (or the fallback output).
func switchMapping(c *MapContext, rules []switchRule, outputs, fallback int) Mapping {
	none := "none"
	fb := none
	if fallback >= 0 {
		fb = strconv.Itoa(fallback)
	}
	var b strings.Builder
	for i, r := range rules {
		tmpl, err := c.condTemplate(fmt.Sprintf("rules[%d]", i), r.set)
		if err != nil {
			return switchViaCode(c, rules, outputs, fb, err)
		}
		kw := "else if"
		if i == 0 {
			kw = "if"
		}
		fmt.Fprintf(&b, "{{ %s %s }}%d", kw, tmpl, r.output)
	}
	field := fb
	if len(rules) > 0 {
		field = b.String() + "{{ else }}" + fb + "{{ end }}"
	}
	cases := make([]interface{}, outputs)
	for i := range cases {
		cases[i] = strconv.Itoa(i)
	}
	return Mapping{Type: "core.switch", Outputs: switchHandles(outputs),
		Config: map[string]interface{}{"field": field, "cases": cases, "default_handle": none}}
}

// switchViaCode computes the output index in JavaScript, then routes on it.
func switchViaCode(c *MapContext, rules []switchRule, outputs int, fb string, cause error) Mapping {
	var b strings.Builder
	b.WriteString(jsValueHelpers + jsCondHelpers + "var o = Object.assign({}, $json);\no.__n8n_output = " + jsQuote(fb) + ";\n")
	for i, r := range rules {
		js, ok := jsCondSet(r.set)
		if !ok {
			c.exprs = append(c.exprs, ExprIssue{Param: fmt.Sprintf("rules[%d]", i), Expression: compactValue(r.set.conds), Reason: cause.Error()})
			js = "false"
		}
		kw := "else if"
		if i == 0 {
			kw = "if"
		}
		fmt.Fprintf(&b, "%s (%s) { o.__n8n_output = %s; }\n", kw, js, jsQuote(strconv.Itoa(r.output)))
	}
	b.WriteString("return o;\n")
	c.Warn("rules", nil, "evaluated in a core.code step (%s); items leaving this node carry the helper field __n8n_output", cause)
	cases := make([]interface{}, outputs)
	for i := range cases {
		cases[i] = strconv.Itoa(i)
	}
	return Mapping{Type: "core.code", Config: codeConfig(wrapPerItem(b.String())), EntrySuffix: " (evaluate)",
		Outputs: switchHandles(outputs),
		Chain: []ChainStep{{Type: "core.switch", Config: map[string]interface{}{
			"field": "{{ $json.__n8n_output }}", "cases": cases, "default_handle": "none"}}}}
}

// switchExpression maps mode "expression": the expression is the output index.
func switchExpression(c *MapContext) Mapping {
	outputs := 4
	switch {
	case c.version(3):
		outputs = int(c.Float("numberOutputs", 4))
	case c.version(2):
		outputs = int(c.Float("outputsAmount", 4))
	}
	field := c.Str("output", "0")
	cases := make([]interface{}, outputs)
	for i := range cases {
		cases[i] = strconv.Itoa(i)
	}
	return Mapping{Type: "core.switch", Outputs: switchHandles(outputs),
		Config: map[string]interface{}{"field": field, "cases": cases, "default_handle": "none"}}
}
