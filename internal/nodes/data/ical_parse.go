package data

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// An RFC 5545 reader for data.extract_from_file's "ics" operation. It turns
// a VCALENDAR into JSON shaped like n8n's (camelCase keys: summary, start.date,
// recurrenceRule, …), so expressions written against n8n output keep working.

type icsProp struct {
	name   string
	params map[string]string
	value  string
}

// parseICS parses a calendar into {version, prodId, method, name, events}.
func parseICS(src string) (map[string]interface{}, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	src = strings.NewReplacer("\n ", "", "\n\t", "").Replace(src)

	cal := map[string]interface{}{}
	events := []interface{}{}
	var stack []string
	var event, alarm map[string]interface{}
	var alarms []interface{}
	sawCalendar := false

	for _, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		p, ok := parseICSLine(line)
		if !ok {
			continue
		}
		switch p.name {
		case "BEGIN":
			comp := strings.ToUpper(p.value)
			stack = append(stack, comp)
			switch comp {
			case "VCALENDAR":
				sawCalendar = true
			case "VEVENT":
				event, alarms = map[string]interface{}{}, nil
			case "VALARM":
				alarm = map[string]interface{}{}
			}
			continue
		case "END":
			comp := strings.ToUpper(p.value)
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			switch {
			case comp == "VALARM" && alarm != nil:
				alarms = append(alarms, alarm)
				alarm = nil
			case comp == "VEVENT" && event != nil:
				if len(alarms) > 0 {
					event["alarms"] = alarms
				}
				events = append(events, event)
				event = nil
			}
			continue
		}
		if len(stack) == 0 {
			continue
		}
		switch stack[len(stack)-1] {
		case "VCALENDAR":
			setCalendarProp(cal, p)
		case "VEVENT":
			if event != nil {
				setEventProp(event, p)
			}
		case "VALARM":
			if alarm != nil {
				key := icsKey(p.name)
				if p.name == "DESCRIPTION" || p.name == "SUMMARY" {
					alarm[key] = icsUnescape(p.value)
				} else {
					alarm[key] = p.value
				}
			}
		}
	}
	if !sawCalendar {
		return nil, errors.New("not an iCalendar file: no BEGIN:VCALENDAR")
	}
	cal["events"] = events
	return cal, nil
}

func setCalendarProp(cal map[string]interface{}, p icsProp) {
	switch p.name {
	case "VERSION":
		cal["version"] = p.value
	case "PRODID":
		cal["prodId"] = p.value
	case "METHOD":
		cal["method"] = p.value
	case "CALSCALE":
		cal["calScale"] = p.value
	case "X-WR-CALNAME":
		cal["name"] = icsUnescape(p.value)
	case "X-WR-TIMEZONE":
		cal["timezone"] = p.value
	}
}

func setEventProp(ev map[string]interface{}, p icsProp) {
	switch p.name {
	case "SUMMARY", "DESCRIPTION", "LOCATION", "COMMENT":
		ev[icsKey(p.name)] = icsUnescape(p.value)
	case "UID", "URL", "STATUS", "CLASS", "DURATION":
		ev[icsKey(p.name)] = p.value
	case "TRANSP":
		ev["transparency"] = p.value
	case "SEQUENCE", "PRIORITY":
		if n, err := strconv.Atoi(p.value); err == nil {
			ev[icsKey(p.name)] = n
		}
	case "DTSTART":
		ev["start"] = icsDate(p)
	case "DTEND":
		ev["end"] = icsDate(p)
	case "DTSTAMP":
		ev["stamp"] = icsDate(p)
	case "CREATED":
		ev["created"] = icsDate(p)
	case "LAST-MODIFIED":
		ev["lastModified"] = icsDate(p)
	case "RECURRENCE-ID":
		ev["recurrenceId"] = icsDate(p)
	case "RRULE":
		ev["recurrenceRule"] = p.value
	case "EXDATE":
		var dates []interface{}
		if prev, ok := ev["exceptionDates"].([]interface{}); ok {
			dates = prev
		}
		for _, v := range strings.Split(p.value, ",") {
			dates = append(dates, icsDate(icsProp{name: p.name, params: p.params, value: v}))
		}
		ev["exceptionDates"] = dates
	case "CATEGORIES":
		var cats []interface{}
		if prev, ok := ev["categories"].([]interface{}); ok {
			cats = prev
		}
		for _, c := range splitICSList(p.value) {
			cats = append(cats, c)
		}
		ev["categories"] = cats
	case "GEO":
		if lat, lon, ok := strings.Cut(p.value, ";"); ok {
			la, err1 := strconv.ParseFloat(lat, 64)
			lo, err2 := strconv.ParseFloat(lon, 64)
			if err1 == nil && err2 == nil {
				ev["geo"] = map[string]interface{}{"lat": la, "lon": lo}
			}
		}
	case "ORGANIZER":
		ev["organizer"] = icsPerson(p)
	case "ATTENDEE":
		var list []interface{}
		if prev, ok := ev["attendees"].([]interface{}); ok {
			list = prev
		}
		ev["attendees"] = append(list, icsPerson(p))
	case "X-MICROSOFT-CDO-BUSYSTATUS":
		ev["busyStatus"] = p.value
	default:
		if strings.HasPrefix(p.name, "X-") {
			ns, _ := ev["nonStandard"].(map[string]interface{})
			if ns == nil {
				ns = map[string]interface{}{}
				ev["nonStandard"] = ns
			}
			ns[p.name] = icsUnescape(p.value)
		}
	}
}

// icsKey turns a property name into the camelCase JSON key
// (LAST-MODIFIED -> lastModified).
func icsKey(name string) string {
	parts := strings.Split(strings.ToLower(name), "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

func icsPerson(p icsProp) map[string]interface{} {
	out := map[string]interface{}{}
	email := p.value
	if len(email) >= 7 && strings.EqualFold(email[:7], "mailto:") {
		email = email[7:]
	}
	out["email"] = email
	if cn := p.params["CN"]; cn != "" {
		out["name"] = cn
	}
	if v := p.params["RSVP"]; v != "" {
		out["rsvp"] = strings.EqualFold(v, "TRUE")
	}
	if v := p.params["ROLE"]; v != "" {
		out["role"] = v
	}
	if v := p.params["PARTSTAT"]; v != "" {
		out["partstat"] = v
	}
	return out
}

// icsDate converts a DATE or DATE-TIME value to {date, type[, local]}, with
// date as an RFC 3339 UTC instant. A TZID'd local time is converted using
// the named zone and kept as local.{date, timezone}.
func icsDate(p icsProp) map[string]interface{} {
	v := strings.TrimSpace(p.value)
	out := map[string]interface{}{}
	if strings.EqualFold(p.params["VALUE"], "DATE") || len(v) == 8 {
		if t, err := time.Parse("20060102", v); err == nil {
			out["type"] = "DATE"
			out["date"] = t.UTC().Format(time.RFC3339)
			return out
		}
	}
	out["type"] = "DATE-TIME"
	if t, err := time.Parse("20060102T150405Z", v); err == nil {
		out["date"] = t.UTC().Format(time.RFC3339)
		return out
	}
	loc := time.UTC
	tzid := p.params["TZID"]
	if tzid != "" {
		if l, err := time.LoadLocation(tzid); err == nil {
			loc = l
		}
	}
	t, err := time.ParseInLocation("20060102T150405", v, loc)
	if err != nil {
		out["date"] = v // unparseable: hand the raw value through
		return out
	}
	out["date"] = t.UTC().Format(time.RFC3339)
	if tzid != "" {
		out["local"] = map[string]interface{}{"date": t.Format("2006-01-02T15:04:05"), "timezone": tzid}
	}
	return out
}

// parseICSLine splits "NAME;P1=a;P2=\"b:c\":value".
func parseICSLine(line string) (icsProp, bool) {
	p := icsProp{params: map[string]string{}}
	i := strings.IndexAny(line, ";:")
	if i <= 0 {
		return p, false
	}
	p.name = strings.ToUpper(line[:i])
	rest := line[i:]
	for strings.HasPrefix(rest, ";") {
		rest = rest[1:]
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			return p, false
		}
		key := strings.ToUpper(rest[:eq])
		rest = rest[eq+1:]
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				return p, false
			}
			val, rest = rest[1:end+1], rest[end+2:]
		} else {
			end := strings.IndexAny(rest, ";:")
			if end < 0 {
				return p, false
			}
			val, rest = rest[:end], rest[end:]
		}
		p.params[key] = val
	}
	if !strings.HasPrefix(rest, ":") {
		return p, false
	}
	p.value = rest[1:]
	return p, true
}

// icsUnescape reverses TEXT escaping.
func icsUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// splitICSList splits a TEXT list on unescaped commas.
func splitICSList(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == ',' {
			out = append(out, icsUnescape(s[start:i]))
			start = i + 1
		}
	}
	return append(out, icsUnescape(s[start:]))
}
