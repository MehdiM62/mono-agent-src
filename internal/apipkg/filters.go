package apipkg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"net/url"
	"strconv"
	"strings"
)

var knownFilters = map[string]bool{
	"json": true, "csv": true, "int": true, "float": true, "bool": true,
	"string": true, "lower": true, "upper": true, "default": true, "rl": true,
	"then": true, "eq": true, "ne": true, "kv": true, "omit": true,
	"fromjson": true, "epoch_ms": true, "epoch": true, "split": true, "date": true,
	"prefix": true, "suffix": true, "base64": true, "base64url": true, "row": true,
	"gid": true, "find": true, "find_row": true,
	"add": true, "sub": true, "colnum": true, "colname": true, "get": true, "sheetgid": true, "entries": true, "path": true,
	"lines": true, "escape_html": true, "join": true, "mul": true, "numeric": true,
	"firestore_value": true, "firestore_fields": true, "pick": true,
	"replace": true, "re": true, "zip": true, "pluck": true, "hex": true, "values": true, "snake": true, "matches": true, "in": true, "flatten": true, "unflatten": true,
	"len": true, "urlencode": true,
	"md5": true, "sha1": true, "sha256": true, "mod": true, "weekday": true, "dotnet_date": true,
	"wrap": true, "mime_parts": true, "sealbox": true,
	"unique": true, "set": true, "not": true, "bind": true, "base64_chunks": true,
	"map": true,
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
		if unset(v) {
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
	case "set":
		return setKey(v, arg) // nil is {}: the object is built from nothing
	case "not":
		if unset(v) {
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
	case "len":
		return lengthOf(v), nil
	case "map":
		return mapThrough(v, stringify(arg)), nil
	case "urlencode":
		return urlEncode(v), nil
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
	case "prefix", "suffix", "escape_html":
		// Lists are handled element-wise (lines | escape_html | prefix:'<p>').
		if l, ok := v.([]any); ok {
			out := make([]any, 0, len(l))
			for _, el := range l {
				r, err := applyFilter(name, el, arg)
				if err != nil {
					return nil, err
				}
				if !isEmpty(r) {
					out = append(out, r)
				}
			}
			return out, nil
		}
		if isEmpty(v) {
			return nil, nil
		}
		switch name {
		case "prefix":
			return stringify(arg) + stringify(v), nil
		case "suffix":
			return stringify(v) + stringify(arg), nil
		}
		return html.EscapeString(stringify(v)), nil
	case "firestore_value":
		return firestoreValue(v), nil
	case "firestore_fields":
		obj, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("firestore_fields: value is %T, not an object", v)
		}
		var cols []string
		if a := stringify(arg); a != "" {
			for _, c := range strings.Split(a, ",") {
				if c = strings.TrimSpace(c); c != "" {
					cols = append(cols, c)
				}
			}
		}
		return firestoreFields(obj, cols), nil
	case "matches":
		re, err := cachedRegexp(stringify(arg))
		if err != nil {
			return nil, fmt.Errorf("matches: %w", err)
		}
		if re.MatchString(stringify(v)) {
			return true, nil
		}
		return nil, nil
	case "in":
		var set []any
		if l, ok := arg.([]any); ok {
			set = l
		} else {
			for _, x := range strings.Split(stringify(arg), ",") {
				set = append(set, strings.TrimSpace(x))
			}
		}
		for _, x := range set {
			if stringify(x) == stringify(v) {
				return true, nil
			}
		}
		return nil, nil
	case "unflatten":
		m, ok := v.(map[string]any)
		if !ok {
			return v, nil
		}
		out := map[string]any{}
		for _, k := range sortedKeys(m) {
			setPath(out, k, m[k])
		}
		return out, nil
	case "flatten":
		// Lists flatten at every depth; objects of lists only at the top
		// (Rocket.Chat {update, remove}); objects inside lists stay elements.
		var out []any
		var add func(x any, top bool)
		add = func(x any, top bool) {
			switch t := x.(type) {
			case []any:
				for _, e := range t {
					add(e, false)
				}
			case map[string]any:
				if !top {
					out = append(out, t)
					return
				}
				for _, k := range sortedKeys(t) {
					add(t[k], false)
				}
			default:
				if x != nil {
					out = append(out, x)
				}
			}
		}
		add(v, true)
		return out, nil
	case "snake":
		str := stringify(v)
		if stringify(arg) == "ascii" {
			str = foldNonASCII(str)
		}
		return snakeCase(str), nil
	case "values":
		m, ok := v.(map[string]any)
		if !ok {
			return v, nil
		}
		return objectValues(m), nil
	case "hex":
		h := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(stringify(v)), "#"), "0x")
		if h == "" {
			return nil, nil
		}
		n, err := strconv.ParseInt(h, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("hex: %q is not hex", stringify(v))
		}
		return n, nil
	case "pluck":
		l, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("pluck: value is %T, not a list", v)
		}
		path := strings.Split(stringify(arg), ".")
		var out []any
		for _, el := range l {
			if x := lookupPath(el, path); !isEmpty(x) {
				out = append(out, x)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	case "zip":
		a := arg.([]any)
		hk, vk := "", ""
		if len(a) > 1 {
			hk = stringify(a[1])
		}
		if len(a) > 2 {
			vk = stringify(a[2])
		}
		return zipLists(a[0], v, hk, vk), nil
	case "replace":
		a := arg.([]any)
		return strings.ReplaceAll(stringify(v), stringify(a[0]), stringify(a[1])), nil
	case "re":
		re, err := cachedRegexp(stringify(arg))
		if err != nil {
			return nil, fmt.Errorf("re: %w", err)
		}
		s := stringify(v)
		m := re.FindStringSubmatch(s)
		switch {
		case m == nil:
			return v, nil
		case len(m) > 1:
			return m[1], nil
		}
		return m[0], nil
	case "lines":
		var out []any
		for _, ln := range strings.Split(strings.ReplaceAll(stringify(v), "\r\n", "\n"), "\n") {
			if t := strings.TrimSpace(ln); t != "" {
				out = append(out, t)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	case "join":
		l, ok := v.([]any)
		if !ok {
			return stringify(v), nil
		}
		parts := make([]string, len(l))
		for i, el := range l {
			parts[i] = stringify(el)
		}
		return strings.Join(parts, stringify(arg)), nil
	case "numeric":
		if s, ok := v.(string); ok {
			if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
				if f == math.Trunc(f) {
					return int64(f), nil
				}
				return f, nil
			}
		}
		return v, nil
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(stringify(v))), nil
	case "base64url":
		return base64.RawURLEncoding.EncodeToString([]byte(stringify(v))), nil
	case "row":
		args := arg.([]any)
		nullMissing := len(args) > 1 && (args[1] == nil || stringify(args[1]) == "null")
		return toRow(v, args[0], nullMissing)
	case "path":
		// Escape each segment but keep the slashes of resource names
		// (spaces/AAA/messages/BBB) in a path template.
		// path:'query' keeps a "?query" suffix as is (a server-relative
		// link with its own query, e.g. Confluence's downloadLink).
		p, query := stringify(v), ""
		if stringify(arg) == "query" {
			if i := strings.Index(p, "?"); i >= 0 {
				p, query = p[:i], p[i:]
			}
		}
		segs := strings.Split(p, "/")
		for i, sg := range segs {
			segs[i] = url.PathEscape(sg)
		}
		return pathValue(strings.Join(segs, "/") + query), nil
	case "entries":
		return toEntries(v, arg)
	case "sheetgid":
		return sheetGID(v), nil
	case "gid":
		return googleFileID(v), nil
	case "add", "sub", "mul":
		a, err := toFloat(v)
		if err != nil || a == nil {
			return nil, err
		}
		b, err := toFloat(arg)
		if err != nil || b == nil {
			return nil, fmt.Errorf("%s: argument %v is not a number", name, arg)
		}
		x, y := a.(float64), b.(float64)
		var r float64
		switch name {
		case "sub":
			r = x - y
		case "mul":
			r = x * y
		default:
			r = x + y
		}
		if r == math.Trunc(r) {
			return int64(r), nil
		}
		return r, nil
	case "get":
		return lookupPath(v, strings.Split(stringify(arg), ".")), nil
	case "colnum":
		return columnNumber(stringify(v))
	case "colname":
		n, err := toInt(v)
		if err != nil || n == nil {
			return nil, err
		}
		return columnName(n.(int64))
	case "find":
		a := arg.([]any)
		return findIn(v, stringify(a[0]), a[1]), nil
	case "find_row":
		a := arg.([]any)
		return findRow(v, stringify(a[0]), a[1]), nil
	case "date":
		return formatDate(v, arg)
	case "epoch_ms":
		return toEpoch(v, true)
	case "epoch":
		return toEpoch(v, false)
	case "omit":
		return omitKeys(v, arg)
	case "pick":
		return pickKeys(v, arg)
	case "md5", "sha1", "sha256":
		return hashHex(name, v, arg)
	case "mod":
		return modulo(v, arg)
	case "sealbox":
		return sealBox(v, arg)
	case "weekday":
		return isoWeekday(v)
	case "dotnet_date":
		return dotnetDates(v), nil
	case "wrap":
		return wrapLines(v, arg)
	case "mime_parts":
		return mimeParts(v, arg)
	case "unique":
		return uniqueList(v, arg)
	case "bind":
		return bindParams(v, arg)
	case "base64_chunks":
		return base64Chunks(v, arg)
	case "lower":
		return strings.ToLower(stringify(v)), nil
	case "upper":
		return strings.ToUpper(stringify(v)), nil
	}
	return nil, fmt.Errorf("unknown filter %q", name)
}
