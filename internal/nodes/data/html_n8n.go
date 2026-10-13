package data

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/monoes/mono-agent/internal/workflow"
)

// n8n-compatible HTML operations (n8n HTML node v1.2):
// extract_html_content, generate_html_template and convert_to_html_table.
// The older extract/extract_all/text/generate operations (html.go) are
// unchanged.

// htmlPerItemFields are evaluated once per item by the node itself (see
// HTMLNode.PerItemConfigFields): the template and the per-row table
// attributes may reference the current item's $json.
var htmlPerItemFields = []string{"html", "row_attributes", "cell_attributes"}

// htmlExtractionValue is one entry of extraction_values.
type htmlExtractionValue struct {
	Key           string
	CSSSelector   string
	ReturnValue   string // text | html | attribute | value
	Attribute     string
	SkipSelectors []string
	ReturnArray   bool
}

// parseHTMLExtractionValues reads extraction_values: a list of objects
// (or n8n's {"values": [...]} wrapper, or a JSON string of either). Each
// object accepts snake_case keys and n8n's camelCase names.
func parseHTMLExtractionValues(config map[string]interface{}) ([]htmlExtractionValue, error) {
	if wrapper, ok := config["extraction_values"].(map[string]interface{}); ok {
		config = map[string]interface{}{"extraction_values": wrapper["values"]}
	} else if s, ok := config["extraction_values"].(string); ok && strings.HasPrefix(strings.TrimSpace(s), "{") {
		obj, err := cfgObject(config, "extraction_values")
		if err != nil {
			return nil, err
		}
		config = map[string]interface{}{"extraction_values": obj["values"]}
	}
	list, err := cfgList(config, "extraction_values")
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%w: extraction_values: at least one value to extract is required", workflow.ErrInvalidConfig)
	}
	out := make([]htmlExtractionValue, 0, len(list))
	for i, raw := range list {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%w: extraction_values[%d]: expected an object, got %T", workflow.ErrInvalidConfig, i, raw)
		}
		pick := func(snake, camel string) string {
			if v := cfgString(m, snake, ""); v != "" {
				return v
			}
			return cfgString(m, camel, "")
		}
		v := htmlExtractionValue{
			Key:           pick("key", "key"),
			CSSSelector:   pick("css_selector", "cssSelector"),
			ReturnValue:   pick("return_value", "returnValue"),
			Attribute:     pick("attribute", "attribute"),
			SkipSelectors: cfgNames(map[string]interface{}{"s": pick("skip_selectors", "skipSelectors")}, "s"),
			ReturnArray:   cfgBool(m, "return_array", cfgBool(m, "returnArray", false)),
		}
		if v.ReturnValue == "" {
			v.ReturnValue = "text"
		}
		switch {
		case v.Key == "":
			return nil, fmt.Errorf("%w: extraction_values[%d].key is required", workflow.ErrInvalidConfig, i)
		case v.ReturnValue != "text" && v.ReturnValue != "html" && v.ReturnValue != "attribute" && v.ReturnValue != "value":
			return nil, fmt.Errorf("%w: extraction_values[%d].return_value: %q is not one of text, html, attribute, value", workflow.ErrInvalidConfig, i, v.ReturnValue)
		case v.ReturnValue == "attribute" && v.Attribute == "":
			return nil, fmt.Errorf("%w: extraction_values[%d].attribute is required when return_value is attribute", workflow.ErrInvalidConfig, i)
		}
		out = append(out, v)
	}
	return out, nil
}

// executeExtractHTMLContent emits one item per HTML document found on each
// input item, holding only the extracted keys.
func executeExtractHTMLContent(input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	values, err := parseHTMLExtractionValues(config)
	if err != nil {
		return nil, err
	}
	source := cfgString(config, "source_data", "json")
	if source != "json" && source != "binary" {
		return nil, fmt.Errorf("%w: source_data: %q is not json or binary", workflow.ErrInvalidConfig, source)
	}
	prop := cfgString(config, "data_property_name", "data")
	trim := cfgBool(config, "trim_values", false)
	clean := cfgBool(config, "clean_up_text", false)

	out := make([]workflow.Item, 0, len(input.Items))
	for i, item := range input.Items {
		docs, err := htmlSourceDocuments(item, source, prop)
		if err != nil {
			return nil, fmt.Errorf("data.html extract_html_content: item %d: %w", i, err)
		}
		for _, src := range docs {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(src))
			if err != nil {
				return nil, fmt.Errorf("data.html extract_html_content: item %d: parse HTML: %w", i, err)
			}
			j := map[string]interface{}{}
			for _, v := range values {
				sel := doc.Find(v.CSSSelector)
				if v.ReturnArray {
					arr := make([]interface{}, 0, sel.Length())
					sel.Each(func(_ int, s *goquery.Selection) {
						if val, ok := htmlExtractValue(s, v, trim, clean); ok {
							arr = append(arr, val)
						} else {
							arr = append(arr, nil)
						}
					})
					j[v.Key] = arr
					continue
				}
				if val, ok := htmlExtractValue(sel, v, trim, clean); ok {
					j[v.Key] = val
				}
			}
			out = append(out, workflow.Item{JSON: j})
		}
	}
	return []workflow.NodeOutput{{Handle: "main", Items: out}}, nil
}

// htmlSourceDocuments returns the HTML strings to extract from: the JSON
// property (a string or a list of strings) or the binary property.
func htmlSourceDocuments(item workflow.Item, source, prop string) ([]string, error) {
	if source == "binary" {
		b, _, err := readBinary(item, prop)
		if err != nil {
			return nil, err
		}
		return []string{string(b)}, nil
	}
	raw, ok := getPath(item.JSON, prop, false)
	if !ok {
		return nil, fmt.Errorf("no property named %q exists (data_property_name)", prop)
	}
	switch v := raw.(type) {
	case string:
		return []string{v}, nil
	case []interface{}:
		docs := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				docs = append(docs, s)
			} else if e != nil {
				docs = append(docs, fmt.Sprintf("%v", e))
			}
		}
		return docs, nil
	case nil:
		return []string{""}, nil
	default:
		return []string{fmt.Sprintf("%v", v)}, nil
	}
}

var (
	htmlLineBreaks = regexp.MustCompile(`\r\n|\n|\r`)
	htmlSpaceRuns  = regexp.MustCompile(`\s+`)
)

// htmlExtractValue applies one extraction to sel. ok is false when the
// value is undefined (no element, or no such attribute).
func htmlExtractValue(sel *goquery.Selection, v htmlExtractionValue, trim, clean bool) (string, bool) {
	var val string
	switch v.ReturnValue {
	case "attribute":
		a, ok := sel.Attr(v.Attribute)
		if !ok {
			return "", false
		}
		val = a
	case "html":
		h, err := sel.Html()
		if err != nil || h == "" {
			return "", false
		}
		val = h
	case "value":
		s, ok := htmlFormValue(sel)
		if !ok {
			return "", false
		}
		val = s
	default: // text
		if sel.Length() == 0 {
			break
		}
		clone := sel.First().Clone()
		for _, skip := range v.SkipSelectors {
			clone.Find(skip).Remove()
		}
		val = htmlNodesToPlainText(clone.Get(0))
	}
	if trim {
		val = strings.TrimSpace(val)
	}
	if clean {
		val = strings.TrimSpace(val)
		val = htmlLineBreaks.ReplaceAllString(val, " ")
		val = htmlSpaceRuns.ReplaceAllString(val, " ")
	}
	return val, true
}

// htmlFormValue returns the value of the first form control in sel:
// an input's value, a textarea's text, a select's selected option value(s)
// joined by commas.
func htmlFormValue(sel *goquery.Selection) (string, bool) {
	if sel.Length() == 0 {
		return "", false
	}
	el := sel.First()
	switch goquery.NodeName(el) {
	case "textarea":
		return el.Text(), true
	case "select":
		opts := el.Find("option")
		selected := opts.FilterFunction(func(_ int, o *goquery.Selection) bool { return htmlHasAttr(o.Get(0), "selected") })
		_, multiple := el.Attr("multiple")
		if !multiple {
			if selected.Length() == 0 {
				return "", false
			}
			selected = selected.Last()
		}
		vals := make([]string, 0, selected.Length())
		selected.Each(func(_ int, o *goquery.Selection) {
			if v, ok := o.Attr("value"); ok {
				vals = append(vals, v)
			} else {
				vals = append(vals, o.Text())
			}
		})
		return strings.Join(vals, ","), true
	default:
		return el.Attr("value")
	}
}

// executeGenerateHTMLTemplate renders the html template once per item,
// evaluating its {{ }} expressions against that item.
func executeGenerateHTMLTemplate(input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	tmpl, ok := config["html"]
	if !ok {
		return nil, fmt.Errorf("%w: html: the HTML template is required", workflow.ErrInvalidConfig)
	}
	engine := workflow.NewExpressionEngine()
	out := make([]workflow.Item, 0, len(input.Items))
	for i, item := range input.Items {
		rendered, err := evalPerItemString(engine, tmpl, item, input)
		if err != nil {
			return nil, fmt.Errorf("data.html generate_html_template: item %d: html: %w", i, err)
		}
		out = append(out, workflow.Item{JSON: map[string]interface{}{"html": rendered}})
	}
	return []workflow.NodeOutput{{Handle: "main", Items: out}}, nil
}

// evalPerItemString evaluates a held-back config value against one item.
// Values the engine already resolved (non-strings) are formatted as-is.
func evalPerItemString(engine *workflow.ExpressionEngine, raw interface{}, item workflow.Item, input workflow.NodeInput) (string, error) {
	switch v := raw.(type) {
	case nil:
		return "", nil
	case string:
		return engine.EvaluateString(v, workflow.ExpressionContext{
			JSON:        item.JSON,
			Node:        input.NodeOutputs,
			WorkflowID:  input.WorkflowID,
			ExecutionID: input.ExecutionID,
		})
	default:
		return xmlScalarString(v), nil
	}
}
