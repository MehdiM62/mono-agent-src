package data

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Date formatting with Luxon-style tokens (yyyy-MM-dd HH:mm:ss), the token
// language n8n's Date & Time node and expressions use, so imported format
// strings keep working. Text in single quotes is literal; '' is a quote.

// luxonTokens lists the supported tokens, longest first so greedy matching
// picks "yyyy" before "yy".
var luxonTokens = []string{
	"kkkk", "yyyy", "MMMM", "LLLL", "EEEE", "cccc", "ZZZZ",
	"ooo", "MMM", "LLL", "EEE", "ccc", "SSS", "ZZZ", "kk",
	"yy", "MM", "LL", "dd", "HH", "hh", "mm", "ss", "ZZ", "WW", "qq", "uu",
	"y", "M", "L", "d", "E", "c", "H", "h", "m", "s", "S", "a", "Z", "z", "X", "x", "o", "W", "q", "u",
}

func formatLuxon(t time.Time, layout string) string {
	var b strings.Builder
	for i := 0; i < len(layout); {
		c := layout[i]
		if c == '\'' {
			end := strings.IndexByte(layout[i+1:], '\'')
			if end < 0 {
				b.WriteString(layout[i+1:])
				break
			}
			if end == 0 {
				b.WriteByte('\'')
			} else {
				b.WriteString(layout[i+1 : i+1+end])
			}
			i += end + 2
			continue
		}
		matched := false
		for _, tok := range luxonTokens {
			if strings.HasPrefix(layout[i:], tok) {
				b.WriteString(luxonToken(t, tok))
				i += len(tok)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func luxonToken(t time.Time, tok string) string {
	pad := func(n, w int) string { return fmt.Sprintf("%0*d", w, n) }
	isoYear, isoWeek := t.ISOWeek()
	hour12 := t.Hour() % 12
	if hour12 == 0 {
		hour12 = 12
	}
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	switch tok {
	case "yyyy":
		return pad(t.Year(), 4)
	case "yy":
		return pad(t.Year()%100, 2)
	case "y":
		return strconv.Itoa(t.Year())
	case "kkkk":
		return pad(isoYear, 4)
	case "kk":
		return pad(isoYear%100, 2)
	case "MMMM", "LLLL":
		return t.Month().String()
	case "MMM", "LLL":
		return t.Month().String()[:3]
	case "MM", "LL":
		return pad(int(t.Month()), 2)
	case "M", "L":
		return strconv.Itoa(int(t.Month()))
	case "dd":
		return pad(t.Day(), 2)
	case "d":
		return strconv.Itoa(t.Day())
	case "EEEE", "cccc":
		return t.Weekday().String()
	case "EEE", "ccc":
		return t.Weekday().String()[:3]
	case "E", "c":
		return strconv.Itoa(weekday)
	case "HH":
		return pad(t.Hour(), 2)
	case "H":
		return strconv.Itoa(t.Hour())
	case "hh":
		return pad(hour12, 2)
	case "h":
		return strconv.Itoa(hour12)
	case "mm":
		return pad(t.Minute(), 2)
	case "m":
		return strconv.Itoa(t.Minute())
	case "ss":
		return pad(t.Second(), 2)
	case "s":
		return strconv.Itoa(t.Second())
	case "SSS":
		return pad(t.Nanosecond()/1e6, 3)
	case "S", "u":
		return strconv.Itoa(t.Nanosecond() / 1e6)
	case "uu":
		return pad(t.Nanosecond()/1e7, 2)
	case "a":
		if t.Hour() < 12 {
			return "AM"
		}
		return "PM"
	case "Z":
		_, off := t.Zone()
		return formatOffset(off, false, true)
	case "ZZ":
		_, off := t.Zone()
		return formatOffset(off, true, false)
	case "ZZZ":
		_, off := t.Zone()
		return formatOffset(off, false, false)
	case "ZZZZ", "z":
		name, _ := t.Zone()
		if tok == "z" && t.Location().String() != "Local" {
			return t.Location().String()
		}
		return name
	case "X":
		return strconv.FormatInt(t.Unix(), 10)
	case "x":
		return strconv.FormatInt(t.UnixMilli(), 10)
	case "o":
		return strconv.Itoa(t.YearDay())
	case "ooo":
		return pad(t.YearDay(), 3)
	case "WW":
		return pad(isoWeek, 2)
	case "W":
		return strconv.Itoa(isoWeek)
	case "q":
		return strconv.Itoa((int(t.Month())-1)/3 + 1)
	case "qq":
		return pad((int(t.Month())-1)/3+1, 2)
	}
	return tok
}

// formatOffset renders a UTC offset as +05:30 (colon), +0530, or the short
// +5 / +5:30 form of Luxon's "Z".
func formatOffset(off int, colon, short bool) string {
	sign := "+"
	if off < 0 {
		sign = "-"
		off = -off
	}
	h, m := off/3600, (off%3600)/60
	switch {
	case short && m == 0:
		return fmt.Sprintf("%s%d", sign, h)
	case short:
		return fmt.Sprintf("%s%d:%02d", sign, h, m)
	case colon:
		return fmt.Sprintf("%s%02d:%02d", sign, h, m)
	default:
		return fmt.Sprintf("%s%02d%02d", sign, h, m)
	}
}

// luxonToGoLayout converts a Luxon format to a Go parse layout, for parsing
// dates with a custom input format. Tokens without a Go equivalent are an
// error rather than a silent mis-parse.
func luxonToGoLayout(layout string) (string, error) {
	goTok := map[string]string{
		"yyyy": "2006", "yy": "06", "y": "2006",
		"MMMM": "January", "LLLL": "January", "MMM": "Jan", "LLL": "Jan",
		"MM": "01", "LL": "01", "M": "1", "L": "1",
		"dd": "02", "d": "2",
		"EEEE": "Monday", "cccc": "Monday", "EEE": "Mon", "ccc": "Mon",
		"HH": "15", "hh": "03", "h": "3",
		"mm": "04", "m": "4", "ss": "05", "s": "5",
		"SSS": "000", "a": "PM",
		"ZZ": "-07:00", "ZZZ": "-0700", "ZZZZ": "MST", "z": "MST",
		"ooo": "002",
	}
	var b strings.Builder
	for i := 0; i < len(layout); {
		c := layout[i]
		if c == '\'' {
			end := strings.IndexByte(layout[i+1:], '\'')
			if end < 0 {
				b.WriteString(layout[i+1:])
				break
			}
			b.WriteString(layout[i+1 : i+1+end])
			i += end + 2
			continue
		}
		matched := false
		for _, tok := range luxonTokens {
			if strings.HasPrefix(layout[i:], tok) {
				g, ok := goTok[tok]
				if !ok {
					return "", fmt.Errorf("format token %q is not supported for parsing", tok)
				}
				b.WriteString(g)
				i += len(tok)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), nil
}
