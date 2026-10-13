package n8nimport

import (
	"fmt"
	"regexp"
	"strings"
)

func init() {
	Register("n8n-nodes-base.htmlExtract", mapHTMLExtract)
}

var simpleFieldExpr = regexp.MustCompile(`^=\{\{\s*\$json\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}$`)

// valueField returns the item field a value parameter reads. Mono data
// nodes take a field name; a value that is not exactly {{ $json.x }} is
// first written to a helper field by a core.set step (returned in pre).
func (c *MapContext) valueField(param, helper string) (field string, pre *ChainStep) {
	raw := c.Raw(param)
	if s, ok := raw.(string); ok {
		if m := simpleFieldExpr.FindStringSubmatch(s); m != nil {
			return m[1], nil
		}
	}
	v := c.Translate(param, setValueString(raw))
	return helper, &ChainStep{Type: "core.set", Suffix: " (prepare)", Config: map[string]interface{}{
		"assignments":   []interface{}{map[string]interface{}{"field": helper, "value": v, "type": "string"}},
		"include_input": true,
	}}
}

// withPre puts an optional preparing step in front of a mapping.
func withPre(pre *ChainStep, m Mapping) Mapping {
	if pre == nil {
		return m
	}
	main := ChainStep{Type: m.Type, Config: m.Config}
	return Mapping{Type: pre.Type, Config: pre.Config, EntrySuffix: pre.Suffix, Outputs: m.Outputs,
		Chain: append([]ChainStep{main}, m.Chain...)}
}

func mapHTMLExtract(c *MapContext) Mapping { return htmlExtract(c) }

// htmlExtract becomes one data.html step per extraction value.
func htmlExtract(c *MapContext) Mapping {
	if c.Literal("sourceData", "json") == "binary" {
		c.Warn("sourceData", "binary", "data.html reads HTML from an item field, not from binary data")
	}
	field := c.Literal("dataPropertyName", "data")
	c.Ignore("options.trimValues", "options.cleanUpText")
	var steps []ChainStep
	for i, v := range c.paramList("extractionValues.values") {
		cfg := map[string]interface{}{"operation": "extract", "field": field,
			"selector": c.Translate(fmt.Sprintf("extractionValues[%d].cssSelector", i), v["cssSelector"]), "output_field": fmt.Sprint(v["key"])}
		if arr, _ := v["returnArray"].(bool); arr {
			cfg["operation"] = "extract_all"
		}
		switch rv, _ := v["returnValue"].(string); rv {
		case "attribute":
			cfg["attribute"] = fmt.Sprint(v["attribute"])
		case "value":
			cfg["attribute"] = "value"
		case "html":
			c.Warn(fmt.Sprintf("extractionValues[%d].returnValue", i), "html", "data.html returns the element text, not its inner HTML")
		}
		if _, ok := v["skipSelectors"]; ok {
			c.Warn(fmt.Sprintf("extractionValues[%d].skipSelectors", i), v["skipSelectors"], "has no mono-agent equivalent")
		}
		steps = append(steps, ChainStep{Type: "data.html", Suffix: fmt.Sprintf(" (%v)", v["key"]), Config: cfg})
	}
	if len(steps) == 0 {
		return Mapping{Type: "core.merge", Config: map[string]interface{}{}}
	}
	c.Note("the extracted values are added to the item; n8n returned only the extracted values")
	return Mapping{Type: steps[0].Type, Config: steps[0].Config, EntrySuffix: steps[0].Suffix, Chain: steps[1:]}
}

func dateTimeV1(c *MapContext) Mapping {
	field := c.Literal("dataPropertyName", "data")
	js, ok := jsForValue(c.Raw("value"))
	if !ok {
		js = "undefined"
		c.Warn("value", c.Raw("value"), "reads data a code step cannot reach")
	}
	var expr string
	if c.Literal("action", "format") == "calculate" {
		fn := map[string]string{"add": "plus", "subtract": "minus"}[c.Literal("operation", "add")]
		expr = fmt.Sprintf("DateTime.fromISO(%s).%s({ %s: Number(%s) }).toISO()", js, fn, jsQuote(c.Literal("timeUnit", "days")), jsValue(c.Float("duration", 0)))
	} else {
		f := c.Literal("toFormat", "")
		moment := strings.NewReplacer("YYYY", "yyyy", "DD", "dd")
		expr = fmt.Sprintf("DateTime.fromISO(%s).toFormat(%s)", js, jsQuote(moment.Replace(f)))
		c.Ignore("custom")
		if f == "" {
			expr = fmt.Sprintf("DateTime.fromISO(%s).toISO()", js)
		}
	}
	body := jsValueHelpers + "var o = Object.assign({}, $json);\n__n8nSet(o, " + jsQuote(field) + ", " + expr + ");\nreturn o;\n"
	return Mapping{Type: "core.code", Config: codeConfig(wrapPerItem(body))}
}
