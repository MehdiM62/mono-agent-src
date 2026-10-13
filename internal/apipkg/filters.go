package apipkg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var knownFilters = map[string]bool{
	"json": true, "csv": true, "int": true, "float": true, "bool": true,
	"string": true, "lower": true, "upper": true, "default": true, "rl": true,
	"then": true, "eq": true, "ne": true, "kv": true, "omit": true,
	"fromjson": true, "epoch_ms": true, "epoch": true, "split": true, "date": true,
	"prefix": true, "suffix": true, "base64": true, "base64url": true, "row": true,
	"gid": true,
}

// applyFilter runs one pipe filter. Filters other than default and then pass
// nil through, so an unset param stays unset (and is omitted).
func applyFilter(name string, v, arg any) (any, error) {
	switch name {
	case "default":
		if isEmpty(v) {
			return arg, nil
		}
		return v, nil
	case "then":
		if isEmpty(v) {
			return nil, nil
		}
		return arg, nil
	case "eq":
		if stringify(v) == stringify(arg) {
			return true, nil
		}
		return nil, nil
	case "ne":
		if stringify(v) != stringify(arg) {
			return true, nil
		}
		return nil, nil
	case "rl":
		// n8n resource locator: {"__rl": true, "mode": "id", "value": "x"}.
		// A "url" locator holding a Google Docs/Sheets/Drive/Slides URL
		// yields the file id, which is what the APIs take.
		if m, ok := v.(map[string]any); ok {
			if val, ok := m["value"]; ok {
				if m["mode"] == "url" {
					return googleFileID(val), nil
				}
				return val, nil
			}
		}
		return v, nil
	}
	if v == nil {
		return nil, nil
	}
	switch name {
	case "json":
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("json: %w", err)
		}
		return string(b), nil
	case "csv":
		return toCSV(v), nil
	case "int":
		return toInt(v)
	case "float":
		return toFloat(v)
	case "bool":
		return toBool(v)
	case "string":
		return stringify(v), nil
	case "kv":
		return toKV(v, arg)
	case "fromjson":
		return fromJSON(v)
	case "split":
		return splitString(v, arg)
	case "prefix":
		if isEmpty(v) {
			return nil, nil
		}
		return stringify(arg) + stringify(v), nil
	case "suffix":
		if isEmpty(v) {
			return nil, nil
		}
		return stringify(v) + stringify(arg), nil
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(stringify(v))), nil
	case "base64url":
		return base64.RawURLEncoding.EncodeToString([]byte(stringify(v))), nil
	case "row":
		return toRow(v, arg)
	case "gid":
		return googleFileID(v), nil
	case "date":
		return formatDate(v, arg)
	case "epoch_ms":
		return toEpoch(v, true)
	case "epoch":
		return toEpoch(v, false)
	case "omit":
		return omitKeys(v, arg)
	case "lower":
		return strings.ToLower(stringify(v)), nil
	case "upper":
		return strings.ToUpper(stringify(v)), nil
	}
	return nil, fmt.Errorf("unknown filter %q", name)
}

func toCSV(v any) any {
	switch t := v.(type) {
	case []any:
		parts := make([]string, 0, len(t))
		for _, el := range t {
			parts = append(parts, stringify(el))
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(t, ",")
	}
	return stringify(v)
}

func toInt(v any) (any, error) {
	switch t := v.(type) {
	case float64:
		return int64(math.Trunc(t)), nil
	case int:
		return int64(t), nil
	case int64:
		return t, nil
	case bool:
		if t {
			return int64(1), nil
		}
		return int64(0), nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil, nil
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("int: %q is not a number", t)
		}
		return int64(math.Trunc(f)), nil
	}
	return nil, fmt.Errorf("int: cannot convert %T", v)
}

func toFloat(v any) (any, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("float: %q is not a number", t)
		}
		return f, nil
	}
	return nil, fmt.Errorf("float: cannot convert %T", v)
}

func toBool(v any) (any, error) {
	switch t := v.(type) {
	case bool:
		return t, nil
	case float64:
		return t != 0, nil
	case int:
		return t != 0, nil
	case int64:
		return t != 0, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "":
			return nil, nil
		case "true", "1", "yes", "on":
			return true, nil
		case "false", "0", "no", "off":
			return false, nil
		}
		return nil, fmt.Errorf("bool: %q is not a boolean", t)
	}
	return nil, fmt.Errorf("bool: cannot convert %T", v)
}

// toKV turns a list of {<key>: k, <value>: v} rows (n8n's fixedCollection
// key/value lists) into the object {k: v}. arg is "keyField,valueField".
// A single-key object wrapping the list (e.g. {"fieldValues": [...]}) is
// unwrapped first.
func toKV(v, arg any) (any, error) {
	names := strings.SplitN(stringify(arg), ",", 3)
	if len(names) < 2 || strings.TrimSpace(names[0]) == "" || strings.TrimSpace(names[1]) == "" {
		return nil, fmt.Errorf(`kv: argument must be "keyField,valueField" or "keyField,valueField,keyPrefix", got %q`, stringify(arg))
	}
	kf, vf := strings.TrimSpace(names[0]), strings.TrimSpace(names[1])
	prefix := ""
	if len(names) == 3 {
		prefix = strings.TrimSpace(names[2])
	}
	if m, ok := v.(map[string]any); ok && len(m) == 1 {
		for _, inner := range m {
			v = inner
		}
	}
	rows, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("kv: value is %T, not a list of rows", v)
	}
	out := map[string]any{}
	for i, r := range rows {
		row, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("kv: row %d is %T, not an object", i, r)
		}
		k := stringify(row[kf])
		if k == "" {
			continue
		}
		out[prefix+k] = row[vf]
	}
	return out, nil
}

// omitKeys returns a copy of the object v without the keys named in arg
// (a comma-separated string or a list).
func omitKeys(v, arg any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("omit: value is %T, not an object", v)
	}
	drop := map[string]bool{}
	switch a := arg.(type) {
	case []any:
		for _, x := range a {
			drop[strings.TrimSpace(stringify(x))] = true
		}
	default:
		for _, x := range strings.Split(stringify(a), ",") {
			drop[strings.TrimSpace(x)] = true
		}
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		if !drop[k] {
			out[k] = val
		}
	}
	return out, nil
}

// fromJSON parses a JSON string (n8n's jsonParameters / *Json params);
// non-strings pass through, an empty string is nil.
func fromJSON(v any) (any, error) {
	str, ok := v.(string)
	if !ok {
		return v, nil
	}
	if strings.TrimSpace(str) == "" {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal([]byte(str), &out); err != nil {
		return nil, fmt.Errorf("fromjson: %w", err)
	}
	return out, nil
}

var epochLayouts = []string{
	time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000", "2006-01-02T15:04:05",
	"2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02",
}

// toEpoch turns a date (RFC 3339, n8n's ISO forms, a date, or a number
// that is already an epoch) into Unix milliseconds or seconds.
func toEpoch(v any, ms bool) (any, error) {
	if f, ok := v.(float64); ok {
		return int64(f), nil
	}
	str := strings.TrimSpace(stringify(v))
	if str == "" {
		return nil, nil
	}
	if n, err := strconv.ParseInt(str, 10, 64); err == nil {
		return n, nil
	}
	for _, l := range epochLayouts {
		if t, err := time.Parse(l, str); err == nil {
			if ms {
				return t.UnixMilli(), nil
			}
			return t.Unix(), nil
		}
	}
	return nil, fmt.Errorf("epoch: %q is not a date", str)
}

// splitString turns "a, b,c" into ["a","b","c"] (sep defaults to ",");
// empty parts are dropped and lists pass through.
func splitString(v, arg any) (any, error) {
	if l, ok := v.([]any); ok {
		return l, nil
	}
	sep := stringify(arg)
	if sep == "" {
		sep = ","
	}
	var out []any
	for _, part := range strings.Split(stringify(v), sep) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// dateTokens maps the tokens package authors write (moment/Luxon style,
// longest first) to Go layout pieces.
var dateTokens = []struct{ tok, layout string }{
	{"YYYY", "2006"}, {"yyyy", "2006"}, {"YY", "06"}, {"yy", "06"},
	{"MM", "01"}, {"DD", "02"}, {"dd", "02"}, {"HH", "15"}, {"hh", "03"},
	{"mm", "04"}, {"ss", "05"}, {"SSS", "000"}, {"ZZ", "-07:00"}, {"Z", "Z07:00"}, {"A", "PM"},
}

// formatDate parses a date (the epoch filter's formats, or epoch ms) and
// formats it with tokens like 'YYYY-MM-DD' or 'YYYY-MM-DDTHH:mm:ssZ' in UTC.
func formatDate(v, arg any) (any, error) {
	pattern := stringify(arg)
	if pattern == "" {
		return nil, fmt.Errorf(`date: needs a format like 'YYYY-MM-DD'`)
	}
	var t time.Time
	switch x := v.(type) {
	case float64:
		t = time.UnixMilli(int64(x))
	default:
		str := strings.TrimSpace(stringify(v))
		if str == "" {
			return nil, nil
		}
		ok := false
		for _, l := range epochLayouts {
			if pt, err := time.Parse(l, str); err == nil {
				t, ok = pt, true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("date: %q is not a date", str)
		}
	}
	var b strings.Builder
	for i := 0; i < len(pattern); {
		matched := false
		for _, dt := range dateTokens {
			if strings.HasPrefix(pattern[i:], dt.tok) {
				b.WriteString(t.UTC().Format(dt.layout))
				i += len(dt.tok)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(pattern[i])
			i++
		}
	}
	return b.String(), nil
}

// toRow orders an object's values by a header list (e.g. a sheet's first
// row): {"b":2,"a":1} with ["a","b","c"] → [1,2,""]. arg may also be a
// list of rows, whose first row is used.
func toRow(v, arg any) (any, error) {
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
