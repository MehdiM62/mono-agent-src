package data

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// convert_to_html_table: all input items become one HTML table in a single
// output item {"table": "<table>…</table>"}. Columns are the union of the
// items' keys (each item's new keys in sorted order, items in input order).
// Booleans render as checkboxes. Values are inserted as-is (not escaped),
// so cells and the caption may carry HTML.

const (
	htmlTableDefaultTableStyle  = `style="border-collapse:collapse; font-family:helvetica,arial,sans-serif"`
	htmlTableDefaultHeaderStyle = `style="text-align:left; color:#777; font-weight:normal; border-bottom:1px solid #ddd"`
	htmlTableDefaultCellStyle   = `style="padding:6px 18px 6px 0; border-bottom:1px solid #eee"`
)

func executeConvertToHTMLTable(input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if len(input.Items) == 0 {
		return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{}}}, nil
	}
	custom := cfgBool(config, "custom_styling", false)
	capitalize := cfgBool(config, "capitalize", false)
	caption := cfgString(config, "caption", "")
	tableStyle, headerStyle, cellStyle := htmlTableDefaultTableStyle, htmlTableDefaultHeaderStyle, htmlTableDefaultCellStyle
	if custom {
		tableStyle, headerStyle, cellStyle = "", "", ""
	}

	var headers []string
	seen := map[string]bool{}
	for _, item := range input.Items {
		keys := make([]string, 0, len(item.JSON))
		for k := range item.JSON {
			if !seen[k] {
				keys = append(keys, k)
				seen[k] = true
			}
		}
		sort.Strings(keys)
		headers = append(headers, keys...)
	}

	engine := workflow.NewExpressionEngine()
	var b strings.Builder
	b.WriteString(htmlOpenTag("table", tableStyle, cfgString(config, "table_attributes", "")))
	if caption != "" {
		b.WriteString("<caption>" + caption + "</caption>")
	}
	b.WriteString(htmlOpenTag("thead", headerStyle, cfgString(config, "header_attributes", "")))
	b.WriteString("<tr>")
	for _, h := range headers {
		b.WriteString("<th>" + capitalizeHTMLHeader(h, capitalize) + "</th>")
	}
	b.WriteString("</tr></thead><tbody>")
	for i, item := range input.Items {
		rowAttrs, err := evalPerItemString(engine, config["row_attributes"], item, input)
		if err != nil {
			return nil, fmt.Errorf("data.html convert_to_html_table: item %d: row_attributes: %w", i, err)
		}
		cellAttrs, err := evalPerItemString(engine, config["cell_attributes"], item, input)
		if err != nil {
			return nil, fmt.Errorf("data.html convert_to_html_table: item %d: cell_attributes: %w", i, err)
		}
		b.WriteString(htmlOpenTag("tr", rowAttrs))
		for _, h := range headers {
			b.WriteString(htmlOpenTag("td", cellStyle, cellAttrs))
			b.WriteString(htmlTableCell(item.JSON[h]))
			b.WriteString("</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table>")
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{{JSON: map[string]interface{}{"table": b.String()}}}}}, nil
}

// htmlOpenTag writes <tag a b> with the non-empty attribute strings.
func htmlOpenTag(tag string, attrs ...string) string {
	parts := []string{tag}
	for _, a := range attrs {
		if a = strings.TrimSpace(a); a != "" {
			parts = append(parts, a)
		}
	}
	return "<" + strings.Join(parts, " ") + ">"
}

func htmlTableCell(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case bool:
		if t {
			return `<input type="checkbox" checked="checked"/>`
		}
		return `<input type="checkbox"/>`
	case map[string]interface{}, []interface{}:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	default:
		return xmlScalarString(t)
	}
}

// capitalizeHTMLHeader turns "first_name" into "First Name" when enabled.
func capitalizeHTMLHeader(h string, capitalize bool) string {
	if !capitalize {
		return h
	}
	var words []string
	for _, w := range strings.Split(h, "_") {
		if w == "" {
			continue
		}
		r := []rune(w)
		words = append(words, strings.ToUpper(string(r[0]))+string(r[1:]))
	}
	return strings.Join(words, " ")
}
