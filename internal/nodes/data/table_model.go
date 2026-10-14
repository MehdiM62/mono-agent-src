package data

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// The table model the spreadsheet-shaped conversions share: items become a
// header plus typed rows (convert_to_file), and rows of typed cells become
// items (extract_from_file).

// flattenJSON flattens nested objects and arrays into dotted keys
// ({"a":{"b":1}} -> {"a.b":1}, {"t":["x"]} -> {"t.0":"x"}), the way n8n's
// spreadsheet export does, so every cell holds a scalar.
func flattenJSON(m map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	var walk func(prefix string, v interface{})
	walk = func(prefix string, v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			if len(t) == 0 {
				out[prefix] = ""
				return
			}
			for k, e := range t {
				walk(prefix+"."+k, e)
			}
		case []interface{}:
			if len(t) == 0 {
				out[prefix] = ""
				return
			}
			for i, e := range t {
				walk(prefix+"."+strconv.Itoa(i), e)
			}
		default:
			out[prefix] = v
		}
	}
	for k, v := range m {
		switch v.(type) {
		case map[string]interface{}, []interface{}:
			walk(k, v)
		default:
			out[k] = v
		}
	}
	return out
}

// itemsTable turns items into column names and rows of scalar cells.
// Columns are every key any item has, sorted the way data.spreadsheet sorts
// them (numeric keys first, in numeric order, then names), because item JSON
// carries no key order.
func itemsTable(items []workflow.Item) ([]string, [][]interface{}) {
	flat := make([]map[string]interface{}, len(items))
	seen := map[string]bool{}
	var cols []string
	for i, it := range items {
		flat[i] = flattenJSON(it.JSON)
		for k := range flat[i] {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}
	sort.Slice(cols, func(i, j int) bool {
		a, aErr := strconv.Atoi(cols[i])
		b, bErr := strconv.Atoi(cols[j])
		if aErr == nil && bErr == nil {
			return a < b
		}
		if (aErr == nil) != (bErr == nil) {
			return aErr == nil
		}
		return cols[i] < cols[j]
	})
	rows := make([][]interface{}, len(flat))
	for i, m := range flat {
		row := make([]interface{}, len(cols))
		for c, k := range cols {
			row[c] = m[k]
		}
		rows[i] = row
	}
	return cols, rows
}

// cellText renders a cell for the text formats (CSV, HTML, RTF).
func cellText(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if math.IsInf(t, 0) || math.IsNaN(t) {
			return fmt.Sprintf("%v", t)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

// cellNumber reports whether v is a Go number and its value.
func cellNumber(v interface{}) (float64, bool) {
	switch v.(type) {
	case float64, float32, int, int64, int32, uint64, json.Number:
		f, ok := toFloat(v)
		return f, ok && !math.IsInf(f, 0) && !math.IsNaN(f)
	}
	return 0, false
}

// tableToRows prepends the header row (when wanted) to typed rows.
func tableToRows(cols []string, rows [][]interface{}, headerRow bool) [][]interface{} {
	out := make([][]interface{}, 0, len(rows)+1)
	if headerRow && len(cols) > 0 {
		h := make([]interface{}, len(cols))
		for i, c := range cols {
			h[i] = c
		}
		out = append(out, h)
	}
	return append(out, rows...)
}

// ── rows -> items ─────────────────────────────────────────────────────────────

// sheetReadOptions are the extract_from_file options shared by every
// spreadsheet-shaped format.
type sheetReadOptions struct {
	HeaderRow         bool
	IncludeEmptyCells bool
	TypedValues       bool // numbers from xlsx/ods cells become JSON numbers
	MaxRows           int  // data rows to return; <0 = all
}

// rowsToSheetItems turns a grid into items the way n8n's spreadsheet reader
// does: with a header row, one object per data row keyed by the header
// (empty cells left out unless IncludeEmptyCells); without one, each row is
// {"row": [cells…]}. Rows with no content at all are skipped.
func rowsToSheetItems(rows [][]interface{}, o sheetReadOptions) []workflow.Item {
	var items []workflow.Item
	var header []string
	start := 0
	if o.HeaderRow {
		for start < len(rows) && rowEmpty(rows[start]) {
			start++
		}
		if start == len(rows) {
			return nil
		}
		for i, h := range rows[start] {
			name := strings.TrimSpace(cellText(h))
			if name == "" {
				name = "__EMPTY"
				if i > 0 {
					name = "__EMPTY_" + strconv.Itoa(i)
				}
			}
			header = append(header, name)
		}
		start++
	}
	for _, row := range rows[start:] {
		if o.MaxRows >= 0 && len(items) >= o.MaxRows {
			break
		}
		if rowEmpty(row) {
			continue
		}
		if !o.HeaderRow {
			cells := make([]interface{}, len(row))
			for i, c := range row {
				cells[i] = sheetValue(c, o.TypedValues)
			}
			items = append(items, workflow.NewItem(map[string]interface{}{"row": cells}))
			continue
		}
		obj := map[string]interface{}{}
		for i, name := range header {
			var c interface{} = ""
			if i < len(row) {
				c = row[i]
			}
			if cellText(c) == "" && !o.IncludeEmptyCells {
				continue
			}
			obj[name] = sheetValue(c, o.TypedValues)
		}
		items = append(items, workflow.NewItem(obj))
	}
	return items
}

func rowEmpty(row []interface{}) bool {
	for _, c := range row {
		if cellText(c) != "" {
			return false
		}
	}
	return true
}

// sheetValue returns a cell as a JSON value: typed cells keep their type
// when typed is set, everything else is its text.
func sheetValue(c interface{}, typed bool) interface{} {
	if !typed {
		return cellText(c)
	}
	switch c.(type) {
	case float64, bool:
		return c
	}
	return cellText(c)
}

// stringGrid widens a [][]string to cells.
func stringGrid(rows [][]string) [][]interface{} {
	out := make([][]interface{}, len(rows))
	for i, r := range rows {
		row := make([]interface{}, len(r))
		for j, c := range r {
			row[j] = c
		}
		out[i] = row
	}
	return out
}

// numericText reports whether a formatted spreadsheet cell is a plain
// number that can be returned as a JSON number without losing anything
// (no leading zeros, no thousands separators, no exponent surprises).
func numericText(s string) (float64, bool) {
	if s == "" || len(s) > 24 {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, strconv.FormatFloat(f, 'f', -1, 64) == s
}
