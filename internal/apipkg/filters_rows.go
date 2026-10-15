package apipkg

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// toRow orders an object's values by a header list (e.g. a sheet's first
// row): {"b":2,"a":1} with ["a","b","c"] → [1,2,""]. arg may also be a
// list of rows, whose first row is used.
func toRow(v, arg any, nullMissing bool) (any, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("row: value is %T, not an object", v)
	}
	header, ok := arg.([]any)
	if ok && len(header) > 0 {
		if first, isRow := header[0].([]any); isRow {
			header = first
		}
	}
	if len(header) == 0 {
		return nil, fmt.Errorf("row: the header is empty (a sheet without a header row?)")
	}
	out := make([]any, len(header))
	for i, h := range header {
		if val, ok := obj[stringify(h)]; ok && val != nil {
			out[i] = val
		} else if nullMissing {
			out[i] = nil // the Sheets API leaves a null cell unchanged
		} else {
			out[i] = ""
		}
	}
	return out, nil
}

// rowsToObjects turns [[h1,h2],[v1,v2],…] into [{h1:v1,h2:v2},…]; short
// rows leave keys out, extra cells are dropped.
func rowsToObjects(v any) any {
	rows, ok := v.([]any)
	if !ok || len(rows) == 0 {
		return v
	}
	header, ok := rows[0].([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(rows)-1)
	for _, r := range rows[1:] {
		cells, _ := r.([]any)
		obj := map[string]any{}
		for i, h := range header {
			if i < len(cells) {
				obj[stringify(h)] = cells[i]
			}
		}
		out = append(out, obj)
	}
	return out
}

var googleIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`/(?:d|folders|file/d|presentation/d|spreadsheets/d|document/d|forms/d)/([A-Za-z0-9_-]{10,})`),
	regexp.MustCompile(`[?&]id=([A-Za-z0-9_-]{10,})`),
}

// googleFileID returns the file id from a Google Docs/Sheets/Slides/Drive
// URL (…/d/<id>/edit, …/folders/<id>, …?id=<id>); anything else (a bare
// id, a non-Google value) passes through unchanged.
func googleFileID(v any) any {
	s, ok := v.(string)
	if !ok || !strings.Contains(s, "google.com") {
		return v
	}
	for _, re := range googleIDPatterns {
		if m := re.FindStringSubmatch(s); m != nil {
			return m[1]
		}
	}
	return v
}

// findIn returns the first element of the list v whose value at the dot
// path equals want (compared as text), or nil.
func findIn(v any, path string, want any) any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	w := stringify(want)
	for _, el := range list {
		if stringify(lookupPath(el, strings.Split(path, "."))) == w {
			return el
		}
	}
	return nil
}

// findRow searches value rows whose first row is the header: it returns the
// 1-based row number (the sheet row when the rows start at row 1) of the
// first data row whose cell in column equals want, or nil. When want is an
// object (e.g. the input item), its value for the column is used.
func findRow(v any, column string, want any) any {
	rows, ok := v.([]any)
	if !ok || len(rows) < 2 {
		return nil
	}
	header, ok := rows[0].([]any)
	if !ok {
		return nil
	}
	col := -1
	for i, h := range header {
		if stringify(h) == column {
			col = i
			break
		}
	}
	if col < 0 {
		return nil
	}
	if m, ok := want.(map[string]any); ok {
		want = m[column]
	}
	w := stringify(want)
	if w == "" {
		return nil
	}
	for i, r := range rows[1:] {
		cells, _ := r.([]any)
		if col < len(cells) && stringify(cells[col]) == w {
			return int64(i + 2)
		}
	}
	return nil
}

// columnNumber turns a spreadsheet column letter into its 1-based number
// ("A" → 1, "AA" → 27).
func columnNumber(s string) (any, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return nil, nil
	}
	n := int64(0)
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return nil, fmt.Errorf("colnum: %q is not a column letter", s)
		}
		n = n*26 + int64(c-'A'+1)
	}
	return n, nil
}

// columnName turns a 1-based column number into its letter (28 → "AB").
func columnName(n int64) (any, error) {
	if n < 1 {
		return nil, fmt.Errorf("colname: %d is not a column number", n)
	}
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{byte('A' + n%26)}, b...)
		n /= 26
	}
	return string(b), nil
}

var gidRe = regexp.MustCompile(`gid=(\d+)`)

// sheetGID turns an n8n sheet locator into the numeric sheet id text:
// "gid=0" (list mode, first sheet), ".../edit#gid=123" (url mode) and
// "123" (id mode / expression) all become the digits. A locator object
// is read at its value. Anything else passes through.
func sheetGID(v any) any {
	if m, ok := v.(map[string]any); ok {
		if val, ok := m["value"]; ok {
			v = val
		}
	}
	s, ok := v.(string)
	if !ok {
		if f, isNum := v.(float64); isNum {
			return strconv.FormatInt(int64(f), 10)
		}
		return v
	}
	if m := gidRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return strings.TrimSpace(s)
}

// toEntries turns an object into [{key: k, value: v}, …] sorted by key;
// arg "keyName,valueName" renames the two keys (Coda: 'column,value').
func toEntries(v, arg any) (any, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("entries: value is %T, not an object", v)
	}
	kn, vn := "key", "value"
	if a := stringify(arg); a != "" {
		parts := strings.SplitN(a, ",", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf(`entries: argument must be "keyName,valueName", got %q`, a)
		}
		kn, vn = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{kn: k, vn: obj[k]})
	}
	return out, nil
}
