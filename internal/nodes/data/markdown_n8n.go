package data

import (
	"bytes"
	"fmt"

	"github.com/monoes/mono-agent/internal/workflow"
)

// n8n-compatible Markdown modes: markdown_to_html and html_to_markdown.
//
// Like n8n, the source text is a value (the `markdown` or `html` key,
// whose {{ }} expressions are evaluated per item) and the result is written
// into a copy of the item at destination_key (a dot path, default "data").
// As a mono convenience the source may instead be read from an item field
// (`field`) when the value is empty; the result then defaults to
// output_field, then to that field. A config without `mode` keeps the
// original behaviour (markdown.go).

// markdownPerItemFields are evaluated once per item by the node itself.
var markdownPerItemFields = []string{"markdown", "html", "destination_key"}

func executeN8nMarkdown(mode string, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	valueKey := "markdown"
	if mode == "html_to_markdown" {
		valueKey = "html"
	}
	field := cfgString(config, "field", "")
	if _, ok := config[valueKey]; !ok && field == "" {
		return nil, fmt.Errorf("%w: %s: the %s to convert is required (or set field to read it from the item)", workflow.ErrInvalidConfig, valueKey, valueKey)
	}

	var convert func(string) (string, error)
	switch mode {
	case "markdown_to_html":
		opts, err := parseMarkdownToHTMLOptions(config)
		if err != nil {
			return nil, err
		}
		md := opts.newConverter()
		convert = func(src string) (string, error) {
			var buf bytes.Buffer
			if err := md.Convert([]byte(src), &buf, opts.parseContext()); err != nil {
				return "", err
			}
			return opts.finish(buf.String()), nil
		}
	case "html_to_markdown":
		opts, err := parseHTMLToMarkdownOptions(config)
		if err != nil {
			return nil, err
		}
		convert = func(src string) (string, error) { return htmlToMarkdown(src, opts) }
	}

	engine := workflow.NewExpressionEngine()
	out := make([]workflow.Item, 0, len(input.Items))
	for i, item := range input.Items {
		src, err := evalPerItemString(engine, config[valueKey], item, input)
		if err != nil {
			return nil, fmt.Errorf("data.markdown %s: item %d: %s: %w", mode, i, valueKey, err)
		}
		dest, err := evalPerItemString(engine, config["destination_key"], item, input)
		if err != nil {
			return nil, fmt.Errorf("data.markdown %s: item %d: destination_key: %w", mode, i, err)
		}
		fromField := false
		if src == "" && field != "" {
			fromField = true
			raw, ok := getPath(item.JSON, field, false)
			if !ok {
				return nil, fmt.Errorf("data.markdown %s: item %d: field %q not found on the item", mode, i, field)
			}
			if raw != nil {
				src = xmlScalarString(raw)
			}
		}
		if dest == "" {
			dest = cfgString(config, "output_field", "")
		}
		if dest == "" && fromField {
			dest = field
		}
		if dest == "" {
			dest = "data"
		}
		result, err := convert(src)
		if err != nil {
			return nil, fmt.Errorf("data.markdown %s: item %d: %w", mode, i, err)
		}
		j := deepCopyMap(item.JSON)
		setPath(j, dest, result, false)
		out = append(out, workflow.Item{JSON: j, Binary: item.Binary})
	}
	return []workflow.NodeOutput{{Handle: "main", Items: out}}, nil
}
