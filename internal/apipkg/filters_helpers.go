package apipkg

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// pathValue is text already escaped for a URL path (the path filter).
type pathValue string

// pickKeys returns a copy of the object v with only the keys named in arg
// (a comma-separated string or a list); listed keys v lacks are skipped.
func pickKeys(v, arg any) (any, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("pick: value is %T, not an object", v)
	}
	var keys []string
	switch a := arg.(type) {
	case []any:
		for _, x := range a {
			keys = append(keys, strings.TrimSpace(stringify(x)))
		}
	default:
		keys = strings.Split(stringify(a), ",")
	}
	out := map[string]any{}
	for _, k := range keys {
		if k = strings.TrimSpace(k); k == "" {
			continue
		}
		if val, ok := m[k]; ok {
			out[k] = val
		}
	}
	return out, nil
}

var reCache sync.Map // pattern → *regexp.Regexp

func cachedRegexp(p string) (*regexp.Regexp, error) {
	if v, ok := reCache.Load(p); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, err
	}
	reCache.Store(p, re)
	return re, nil
}

// zipLists pairs a header list with a value list into an object: key i is
// headers[i] (or headers[i][headerKey]), value i is values[i] (or
// values[i][valueKey]). GA4 rows: {{ el.dimensionValues | zip:res.dimensionHeaders,'name','value' }}.
func zipLists(headers, values any, headerKey, valueKey string) any {
	hs, _ := headers.([]any)
	vs, _ := values.([]any)
	out := map[string]any{}
	for i, h := range hs {
		if i >= len(vs) {
			break
		}
		k := h
		if headerKey != "" {
			k = lookupPath(h, strings.Split(headerKey, "."))
		}
		v := vs[i]
		if valueKey != "" {
			v = lookupPath(v, strings.Split(valueKey, "."))
		}
		if ks := stringify(k); ks != "" {
			out[ks] = v
		}
	}
	return out
}

// epochTime reads a Unix time in seconds or, from 1e11 up (after 1973 in
// ms), milliseconds.
func epochTime(n int64) time.Time {
	if n >= 1e11 || n <= -1e11 {
		return time.UnixMilli(n)
	}
	return time.Unix(n, 0)
}

// truthy is how *_when, until and $when conditions read a rendered value:
// empty, false, 0 and "false"/"0"/"no"/"off" are false; any other
// non-empty value (an id, a mode name) is true.
func truthy(v any) bool {
	if isEmpty(v) {
		return false
	}
	if b, err := toBool(v); err == nil && b != nil {
		return b == true
	}
	return true
}

// snakeCase is change-case's snakeCase (v5, as n8n uses it): words split
// where a lower-case letter or digit meets an upper-case one, before the
// last capital of an acronym followed by a lower-case letter, and on
// anything that is not a letter or ASCII digit; lower-cased and joined by
// "_" ("Due Date", "DueDate", "due-date" → "due_date"; "Q1 Goals" →
// "q1_goals"; letters and digits are not split apart).
func snakeCase(s string) string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	isDigit := func(r rune) bool { return r >= '0' && r <= '9' }
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case unicode.IsLetter(r) || isDigit(r):
			if len(cur) > 0 {
				prev := cur[len(cur)-1]
				lowerToUpper := unicode.IsUpper(r) && (unicode.IsLower(prev) || isDigit(prev))
				acronymEnd := unicode.IsUpper(r) && unicode.IsUpper(prev) && i+1 < len(rs) && unicode.IsLower(rs[i+1])
				if lowerToUpper || acronymEnd {
					flush()
				}
			}
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return strings.Join(words, "_")
}

// foldNonASCII turns every character outside printable ASCII into a space,
// so snake gives the older ASCII-only keys ("Prénom" → "pr_nom"; n8n
// Notion before node version 3).
func foldNonASCII(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return ' '
		}
		return r
	}, s)
}

// unset is what then treats as "no value": empty, false and the text
// "false" (n8n boolean toggles arrive as either). 0 is a value.
func unset(v any) bool {
	return isEmpty(v) || v == false || stringify(v) == "false"
}
