package data

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Duration between two dates broken down into the requested units, like
// Luxon's DateTime.diff(units): calendar units (year, quarter, month, week,
// day) are counted by stepping the calendar, the rest by elapsed time, and
// whatever is left over becomes a fraction of the smallest unit.

var diffUnitOrder = map[string]int{
	"year": 0, "quarter": 1, "month": 2, "week": 3, "day": 4,
	"hour": 5, "minute": 6, "second": 7, "millisecond": 8,
}

var diffFixed = map[string]time.Duration{
	"hour": time.Hour, "minute": time.Minute, "second": time.Second, "millisecond": time.Millisecond,
}

func dateDiff(start, end time.Time, units []string) (map[string]interface{}, error) {
	norm := make([]string, 0, len(units))
	seen := map[string]bool{}
	for _, u := range units {
		u = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(u)), "s")
		if _, ok := diffUnitOrder[u]; !ok {
			return nil, fmt.Errorf("units: unknown unit %q", u)
		}
		if !seen[u] {
			norm = append(norm, u)
			seen[u] = true
		}
	}
	sort.Slice(norm, func(i, j int) bool { return diffUnitOrder[norm[i]] < diffUnitOrder[norm[j]] })

	sign := 1.0
	if end.Before(start) {
		start, end = end, start
		sign = -1
	}
	out := map[string]interface{}{}
	cursor := start
	for i, u := range norm {
		last := i == len(norm)-1
		var count int
		var next func(time.Time, int) time.Time
		if d, ok := diffFixed[u]; ok {
			remaining := end.Sub(cursor)
			if last {
				out[u+"s"] = sign * float64(remaining) / float64(d)
				break
			}
			count = int(remaining / d)
			cursor = cursor.Add(time.Duration(count) * d)
			out[u+"s"] = sign * float64(count)
			continue
		}
		next = calendarStepper(u)
		for !next(cursor, count+1).After(end) {
			count++
		}
		base := next(cursor, count)
		if last {
			span := next(cursor, count+1).Sub(base)
			frac := 0.0
			if span > 0 {
				frac = float64(end.Sub(base)) / float64(span)
			}
			out[u+"s"] = sign * (float64(count) + frac)
			break
		}
		cursor = base
		out[u+"s"] = sign * float64(count)
	}
	return out, nil
}

func calendarStepper(unit string) func(time.Time, int) time.Time {
	switch unit {
	case "year":
		return func(t time.Time, n int) time.Time { return addMonthsClamped(t, 12*n) }
	case "quarter":
		return func(t time.Time, n int) time.Time { return addMonthsClamped(t, 3*n) }
	case "month":
		return func(t time.Time, n int) time.Time { return addMonthsClamped(t, n) }
	case "week":
		return func(t time.Time, n int) time.Time { return t.AddDate(0, 0, 7*n) }
	default: // day
		return func(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) }
	}
}

// isoDuration renders a diff as an ISO 8601 duration (P1Y2M3W4DT5H6M7.5S).
func isoDuration(d map[string]interface{}) string {
	num := func(k string) float64 { f, _ := d[k].(float64); return f }
	neg := false
	for _, v := range d {
		if f, _ := v.(float64); f < 0 {
			neg = true
		}
	}
	abs := func(f float64) float64 {
		if f < 0 {
			return -f
		}
		return f
	}
	fmtN := func(f float64) string { return strconv.FormatFloat(abs(f), 'f', -1, 64) }
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	b.WriteByte('P')
	for _, p := range []struct{ key, suffix string }{{"years", "Y"}, {"quarters", "Q"}, {"months", "M"}, {"weeks", "W"}, {"days", "D"}} {
		if _, ok := d[p.key]; ok {
			v := num(p.key)
			if p.key == "quarters" {
				b.WriteString(fmtN(v*3) + "M")
				continue
			}
			b.WriteString(fmtN(v) + p.suffix)
		}
	}
	secs := num("seconds") + num("milliseconds")/1000
	_, hasS := d["seconds"]
	_, hasMs := d["milliseconds"]
	_, hasH := d["hours"]
	_, hasM := d["minutes"]
	if hasH || hasM || hasS || hasMs {
		b.WriteByte('T')
		if hasH {
			b.WriteString(fmtN(num("hours")) + "H")
		}
		if hasM {
			b.WriteString(fmtN(num("minutes")) + "M")
		}
		if hasS || hasMs {
			b.WriteString(fmtN(secs) + "S")
		}
	}
	if b.Len() == 1 || (neg && b.Len() == 2) {
		b.WriteString("T0S")
	}
	return b.String()
}
