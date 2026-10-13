package n8nimport

import (
	"strconv"
	"strings"
)

// mono's now helper returns UTC time as RFC 3339 ("2006-01-02T15:04:05Z"),
// so date parts are fixed-position slices of it.
var datePartSlices = map[string][2]int{
	"yyyy": {0, 4}, "yy": {2, 4}, "MM": {5, 7}, "dd": {8, 10},
	"HH": {11, 13}, "mm": {14, 16}, "ss": {17, 19},
}

func (e *emitter) dateNote() {
	e.note("$now/$today are UTC in mono-agent (n8n used the workflow timezone)")
}

// dateValue is a date used as a plain value: an ISO timestamp string.
func (e *emitter) dateValue(v val) val {
	e.dateNote()
	if v.name == "today" {
		return call(tStr, "printf", `"%sT00:00:00Z"`, "(slice (now) 0 10)")
	}
	return val{p: "(now)", t: tStr}
}

// datePart returns the slice of now for a format token, or "00" for a time
// token of $today.
func datePart(v val, tok string) (string, bool) {
	r := datePartSlices[tok]
	if v.name == "today" && r[0] >= 11 {
		return "", false
	}
	return "(slice (now) " + strconv.Itoa(r[0]) + " " + strconv.Itoa(r[1]) + ")", true
}

func (e *emitter) dateMember(v val, key string) (val, error) {
	tok := map[string]string{"year": "yyyy", "month": "MM", "day": "dd", "hour": "HH", "minute": "mm", "second": "ss"}[key]
	if tok == "" {
		return val{}, unsupported("%s.%s", describeObj(v), key)
	}
	e.dateNote()
	part, ok := datePart(v, tok)
	if !ok {
		return numVal(0), nil
	}
	return call(tNum, "toInt", part), nil
}

func (e *emitter) dateMethod(v val, method string, args []*jsNode) (val, error) {
	switch method {
	case "toISO", "toString", "toJSON", "toISOString":
		if len(args) == 0 {
			return e.dateValue(v), nil
		}
	case "toISODate":
		e.dateNote()
		return call(tStr, "slice", "(now)", "0", "10"), nil
	case "toFormat":
		if f, ok := literalString(first(args)); ok && len(args) == 1 {
			return e.formatDate(v, f)
		}
	}
	return val{}, unsupported("%s.%s() (use a data.datetime node)", describeObj(v), method)
}

// formatDate translates a Luxon toFormat pattern built from yyyy, yy, MM,
// dd, HH, mm, ss, quoted literals and punctuation.
func (e *emitter) formatDate(v val, f string) (val, error) {
	var format strings.Builder
	var args []string
	for i := 0; i < len(f); {
		c := f[i]
		switch {
		case c == '\'':
			end := strings.IndexByte(f[i+1:], '\'')
			if end < 0 {
				return val{}, unsupported("format %q", f)
			}
			format.WriteString(strings.ReplaceAll(f[i+1:i+1+end], "%", "%%"))
			i += end + 2
			continue
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			j := i
			for j < len(f) && f[j] == c {
				j++
			}
			tok := f[i:j]
			if _, known := datePartSlices[tok]; !known {
				return val{}, unsupported("date format token %q (use a data.datetime node)", tok)
			}
			if part, ok := datePart(v, tok); ok {
				format.WriteString("%s")
				args = append(args, part)
			} else {
				format.WriteString("00")
			}
			i = j
			continue
		case c == '%':
			format.WriteString("%%")
		default:
			format.WriteByte(c)
		}
		i++
	}
	e.dateNote()
	if len(args) == 1 && format.String() == "%s" {
		return val{p: args[0], t: tStr}, nil
	}
	return call(tStr, "printf", append([]string{strconv.Quote(format.String())}, args...)...), nil
}
