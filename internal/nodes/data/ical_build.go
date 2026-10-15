package data

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// An RFC 5545 (iCalendar) writer for single events, shared by data.ical and
// data.convert_to_file's "ics" operation.

// icalNow is the clock used for DTSTAMP; tests replace it.
var icalNow = time.Now

type icalPerson struct {
	Name  string
	Email string
	RSVP  bool
}

type icalEvent struct {
	Title       string
	Start, End  time.Time
	AllDay      bool
	Description string
	Location    string
	URL         string
	UID         string
	Status      string
	BusyStatus  string
	RRule       string
	Sequence    *int
	Geo         *[2]float64
	Organizer   *icalPerson
	Attendees   []icalPerson
}

// buildICS renders a VCALENDAR holding events. calName becomes
// X-WR-CALNAME, the name calendar apps show for the calendar.
func buildICS(calName string, events []icalEvent) []byte {
	var w icsWriter
	w.line("BEGIN:VCALENDAR")
	w.line("VERSION:2.0")
	w.line("PRODID:-//mono-agent//data.ical//EN")
	w.line("CALSCALE:GREGORIAN")
	w.line("METHOD:PUBLISH")
	if calName != "" {
		w.line("X-WR-CALNAME:" + icsText(calName))
	}
	stamp := icalNow().UTC().Format("20060102T150405Z")
	for _, e := range events {
		w.line("BEGIN:VEVENT")
		w.line("UID:" + icsText(e.UID))
		w.line("DTSTAMP:" + stamp)
		if e.AllDay {
			w.line("DTSTART;VALUE=DATE:" + e.Start.Format("20060102"))
			w.line("DTEND;VALUE=DATE:" + e.End.Format("20060102"))
		} else {
			w.line("DTSTART:" + e.Start.UTC().Format("20060102T150405Z"))
			w.line("DTEND:" + e.End.UTC().Format("20060102T150405Z"))
		}
		w.line("SUMMARY:" + icsText(e.Title))
		if e.Description != "" {
			w.line("DESCRIPTION:" + icsText(e.Description))
		}
		if e.Location != "" {
			w.line("LOCATION:" + icsText(e.Location))
		}
		if e.URL != "" {
			w.line("URL:" + singleLine(e.URL))
		}
		if e.Geo != nil {
			w.line("GEO:" + strconv.FormatFloat(e.Geo[0], 'f', -1, 64) + ";" + strconv.FormatFloat(e.Geo[1], 'f', -1, 64))
		}
		if e.Status != "" {
			w.line("STATUS:" + e.Status)
		}
		if e.Sequence != nil {
			w.line("SEQUENCE:" + strconv.Itoa(*e.Sequence))
		}
		if e.BusyStatus != "" {
			w.line("X-MICROSOFT-CDO-BUSYSTATUS:" + e.BusyStatus)
		}
		if e.RRule != "" {
			w.line("RRULE:" + singleLine(e.RRule))
		}
		if e.Organizer != nil && e.Organizer.Email != "" {
			w.line("ORGANIZER" + icsCN(e.Organizer.Name) + ":mailto:" + singleLine(e.Organizer.Email))
		}
		for _, a := range e.Attendees {
			params := ""
			if a.RSVP {
				params = ";RSVP=TRUE"
			}
			w.line("ATTENDEE" + params + icsCN(a.Name) + ":mailto:" + singleLine(a.Email))
		}
		w.line("END:VEVENT")
	}
	w.line("END:VCALENDAR")
	return w.buf.Bytes()
}

// icsWriter emits content lines ending in CRLF, folded so no physical line
// exceeds 75 octets (RFC 5545 §3.1). A fold never splits a UTF-8 sequence.
type icsWriter struct{ buf bytes.Buffer }

func (w *icsWriter) line(s string) {
	limit := 75
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		w.buf.WriteString(s[:cut])
		w.buf.WriteString("\r\n ")
		s = s[cut:]
		limit = 74 // the leading space of a continuation line counts
	}
	w.buf.WriteString(s)
	w.buf.WriteString("\r\n")
}

// icsText escapes a TEXT value (RFC 5545 §3.3.11).
func icsText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`, "\r", `\n`).Replace(s)
}

// singleLine drops line breaks from a value that has no escaping of its own
// (URI, RECUR), so it cannot start a new property.
func singleLine(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(strings.TrimSpace(s))
}

// icsCN renders a ;CN= parameter, quoted when the name holds characters
// that are special in a parameter value. DQUOTE cannot appear at all.
func icsCN(name string) string {
	name = strings.ReplaceAll(singleLine(name), `"`, "")
	if name == "" {
		return ""
	}
	if strings.ContainsAny(name, ":;,") {
		return `;CN="` + name + `"`
	}
	return ";CN=" + name
}

// parseEventTime accepts RFC 3339 timestamps, the common date-time layouts
// without a zone (read in the local zone, as n8n does), plain dates, and
// Unix epochs (seconds or milliseconds) given as numbers.
func parseEventTime(v interface{}) (t time.Time, dateOnly bool, err error) {
	if f, ok := v.(float64); ok {
		return epochTime(f), false, nil
	}
	if n, ok := v.(int); ok {
		return epochTime(float64(n)), false, nil
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" || v == nil {
		return time.Time{}, false, fmt.Errorf("is empty")
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05Z0700", "2006-01-02T15:04Z07:00", "2006-01-02 15:04:05Z07:00"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, false, nil
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, false, nil
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, true, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return epochTime(f), false, nil
	}
	return time.Time{}, false, fmt.Errorf("%q is not a date-time (want e.g. 2026-10-13T09:30:00Z or 2026-10-13)", s)
}

func epochTime(f float64) time.Time {
	if f > 1e12 {
		return time.UnixMilli(int64(f))
	}
	return time.Unix(int64(f), 0)
}
