package data

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// n8n Date & Time (v2) operations on data.datetime. Unlike the original
// field-based operations, these take the date itself as a value (usually an
// expression such as {{ $json.created }}), evaluated once per item, and
// write the result to output_field_name.

var dateTimeV2Ops = map[string]bool{
	"get_current_date": true, "add_to_date": true, "subtract_from_date": true,
	"format_date": true, "round_date": true, "get_time_between_dates": true, "extract_date": true,
}

// dateTimeV2Defaults are n8n's default output field names per operation.
var dateTimeV2Defaults = map[string]string{
	"get_current_date": "currentDate", "add_to_date": "newDate", "subtract_from_date": "newDate",
	"format_date": "formattedDate", "round_date": "roundedDate",
	"get_time_between_dates": "timeDifference", "extract_date": "datePart",
}

// luxonISO is Luxon's DateTime.toString() shape.
const luxonISO = "2006-01-02T15:04:05.000Z07:00"

// PerItemConfigFields keeps the engine from resolving the v2 date values
// with the first item only; executeV2 evaluates them for every item.
func (n *DateTimeNode) PerItemConfigFields() []string {
	return []string{"date", "magnitude", "start_date", "end_date"}
}

func (n *DateTimeNode) executeV2(input workflow.NodeInput, config map[string]interface{}, op string) ([]workflow.NodeOutput, error) {
	fail := func(err error) ([]workflow.NodeOutput, error) {
		return nil, fmt.Errorf("data.datetime %s: %w", op, err)
	}
	loc := time.Local
	if tz := cfgString(config, "timezone", ""); tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return fail(fmt.Errorf("timezone: %w", err))
		}
		loc = l
	}
	outField := cfgString(config, "output_field_name", dateTimeV2Defaults[op])
	includeInput := cfgBool(config, "include_input_fields", false)
	eval := newItemEvaluator(input)

	out := make([]workflow.Item, 0, len(input.Items))
	for idx, item := range input.Items {
		j := map[string]interface{}{}
		var bin map[string][]byte
		if includeInput {
			j = copyMap(item.JSON)
			bin = item.Binary
		}
		val, err := n.v2Value(op, config, item, eval, loc)
		if err != nil {
			return nil, fmt.Errorf("data.datetime %s: item %d: %w", op, idx, err)
		}
		j[outField] = val
		out = append(out, workflow.Item{JSON: j, Binary: bin})
	}
	return []workflow.NodeOutput{{Handle: "main", Items: out}}, nil
}

func (n *DateTimeNode) v2Value(op string, config map[string]interface{}, item workflow.Item, eval itemEvaluator, loc *time.Location) (interface{}, error) {
	dateArg := func(key string) (time.Time, error) {
		raw, err := eval.value(config[key], item)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s: %w", key, err)
		}
		t, err := parseFlexibleDate(raw, "", loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s: %w", key, err)
		}
		return t, nil
	}
	switch op {
	case "get_current_date":
		now := time.Now().In(loc)
		if !cfgBool(config, "include_time", true) {
			now = startOfUnit(now, "day")
		}
		return now.Format(luxonISO), nil

	case "add_to_date", "subtract_from_date":
		t, err := dateArg("magnitude")
		if err != nil {
			return nil, err
		}
		amount, err := cfgFloat(config, "duration", 0)
		if err != nil {
			return nil, err
		}
		if op == "subtract_from_date" {
			amount = -amount
		}
		res, err := addUnits(t, cfgString(config, "time_unit", "days"), amount)
		if err != nil {
			return nil, err
		}
		return res.Format(luxonISO), nil

	case "format_date":
		raw, err := eval.value(config["date"], item)
		if err != nil {
			return nil, fmt.Errorf("date: %w", err)
		}
		if raw == nil {
			return nil, nil
		}
		t, err := parseFlexibleDate(raw, cfgString(config, "from_format", ""), loc)
		if err != nil {
			return nil, fmt.Errorf("date: %w", err)
		}
		format := cfgString(config, "format", "MM/dd/yyyy")
		if format == "custom" {
			format = cfgString(config, "custom_format", "")
			if format == "" {
				return nil, fmt.Errorf("custom_format is required when format is custom")
			}
		}
		return formatLuxon(t, format), nil

	case "round_date":
		t, err := dateArg("date")
		if err != nil {
			return nil, err
		}
		switch mode := cfgString(config, "mode", "round_down"); mode {
		case "round_down", "roundDown":
			return startOfUnit(t, cfgString(config, "to_nearest", "month")).Format(luxonISO), nil
		case "round_up", "roundUp":
			unit := cfgString(config, "to", "month")
			next, err := addUnits(t, unit+"s", 1)
			if err != nil {
				return nil, err
			}
			return startOfUnit(next, unit).Format(luxonISO), nil
		default:
			return nil, fmt.Errorf("mode: unknown value %q (want round_down or round_up)", mode)
		}

	case "get_time_between_dates":
		start, err := dateArg("start_date")
		if err != nil {
			return nil, err
		}
		end, err := dateArg("end_date")
		if err != nil {
			return nil, err
		}
		units := cfgNames(config, "units")
		if len(units) == 0 {
			units = []string{"day"}
		}
		diff, err := dateDiff(start, end, units)
		if err != nil {
			return nil, err
		}
		if cfgBool(config, "iso_string", false) {
			return isoDuration(diff), nil
		}
		return diff, nil

	case "extract_date":
		t, err := dateArg("date")
		if err != nil {
			return nil, err
		}
		_, week := t.ISOWeek()
		parts := map[string]int{
			"year": t.Year(), "month": int(t.Month()), "week": week, "day": t.Day(),
			"hour": t.Hour(), "minute": t.Minute(), "second": t.Second(),
		}
		part := cfgString(config, "part", "month")
		v, ok := parts[part]
		if !ok {
			return nil, fmt.Errorf("part: unknown value %q", part)
		}
		return v, nil
	}
	return nil, fmt.Errorf("unknown operation %q", op)
}

var flexibleLayouts = []string{
	time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999", "2006-01-02 15:04", "2006-01-02", "2006/01/02", "2006/01/02 15:04:05",
	"01/02/2006", "01/02/2006 15:04:05", time.RFC1123, time.RFC1123Z, time.RFC850, time.RFC822, time.RFC822Z,
	time.ANSIC, time.UnixDate, "Mon Jan 02 2006 15:04:05 GMT-0700", "January 2, 2006", "January 2 2006", "Jan 2, 2006", "2 January 2006",
}

// parseFlexibleDate reads the date shapes n8n accepts: a Unix timestamp
// (seconds when shorter than 12 digits, else milliseconds; fractional =
// seconds), an ISO-like or common textual date, or, with fromFormat, a
// Luxon-formatted string. Dates without an offset are read in loc; the
// result is expressed in loc.
func parseFlexibleDate(raw interface{}, fromFormat string, loc *time.Location) (time.Time, error) {
	if f, isNum := raw.(float64); isNum && fromFormat != "" {
		raw = strconv.FormatFloat(f, 'f', -1, 64)
	}
	switch v := raw.(type) {
	case nil:
		return time.Time{}, fmt.Errorf("no date given")
	case time.Time:
		return v.In(loc), nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return time.Time{}, fmt.Errorf("no date given")
		}
		if fromFormat != "" {
			layout, err := luxonToGoLayout(fromFormat)
			if err != nil {
				return time.Time{}, fmt.Errorf("from_format: %w", err)
			}
			t, err := time.ParseInLocation(layout, s, loc)
			if err != nil {
				return time.Time{}, fmt.Errorf("%q does not match from_format %q", s, fromFormat)
			}
			return t.In(loc), nil
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return timestampToTime(f, strings.Contains(s, ".")).In(loc), nil
		}
		for _, layout := range flexibleLayouts {
			if t, err := time.ParseInLocation(layout, s, loc); err == nil {
				return t.In(loc), nil
			}
		}
		return time.Time{}, fmt.Errorf("invalid date format %q", s)
	default:
		f, ok := toNumber(v)
		if !ok {
			return time.Time{}, fmt.Errorf("cannot read a date from %T", raw)
		}
		return timestampToTime(f, f != math.Trunc(f)).In(loc), nil
	}
}

func timestampToTime(f float64, fractional bool) time.Time {
	if fractional {
		return time.UnixMilli(int64(math.Round(f * 1000)))
	}
	n := int64(f)
	if len(strconv.FormatInt(absInt64(n), 10)) < 12 {
		return time.Unix(n, 0)
	}
	return time.UnixMilli(n)
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// addUnits adds amount of unit (years, quarters, months, weeks, days, hours,
// minutes, seconds, milliseconds; singular accepted). Month arithmetic clamps
// to the last day of the target month as Luxon does (Jan 31 + 1 month = Feb 28/29).
func addUnits(t time.Time, unit string, amount float64) (time.Time, error) {
	unit = strings.TrimSuffix(strings.ToLower(unit), "s")
	whole := int(amount)
	frac := amount - float64(whole)
	switch unit {
	case "year":
		return addMonthsClamped(t, whole*12), nonZeroFrac(frac, unit)
	case "quarter":
		return addMonthsClamped(t, whole*3), nonZeroFrac(frac, unit)
	case "month":
		return addMonthsClamped(t, whole), nonZeroFrac(frac, unit)
	case "week":
		return t.AddDate(0, 0, whole*7).Add(time.Duration(frac * 7 * 24 * float64(time.Hour))), nil
	case "day":
		return t.AddDate(0, 0, whole).Add(time.Duration(frac * 24 * float64(time.Hour))), nil
	case "hour":
		return t.Add(time.Duration(amount * float64(time.Hour))), nil
	case "minute":
		return t.Add(time.Duration(amount * float64(time.Minute))), nil
	case "second":
		return t.Add(time.Duration(amount * float64(time.Second))), nil
	case "millisecond":
		return t.Add(time.Duration(amount * float64(time.Millisecond))), nil
	}
	return t, fmt.Errorf("time_unit: unknown unit %q", unit)
}

func nonZeroFrac(frac float64, unit string) error {
	if frac != 0 {
		return fmt.Errorf("duration: %ss must be a whole number", unit)
	}
	return nil
}

func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	target := time.Date(y, m+time.Month(months), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := time.Date(target.Year(), target.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if d > last {
		d = last
	}
	return time.Date(target.Year(), target.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

// startOfUnit truncates t to the start of its year, quarter, month, week
// (Monday), day, hour, minute or second.
func startOfUnit(t time.Time, unit string) time.Time {
	y, m, d := t.Date()
	loc := t.Location()
	switch strings.TrimSuffix(strings.ToLower(unit), "s") {
	case "year":
		return time.Date(y, 1, 1, 0, 0, 0, 0, loc)
	case "quarter":
		return time.Date(y, ((m-1)/3)*3+1, 1, 0, 0, 0, 0, loc)
	case "month":
		return time.Date(y, m, 1, 0, 0, 0, 0, loc)
	case "week":
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
		return time.Date(y, m, d-(wd-1), 0, 0, 0, 0, loc)
	case "day":
		return time.Date(y, m, d, 0, 0, 0, 0, loc)
	case "hour":
		return time.Date(y, m, d, t.Hour(), 0, 0, 0, loc)
	case "minute":
		return time.Date(y, m, d, t.Hour(), t.Minute(), 0, 0, loc)
	case "second":
		return time.Date(y, m, d, t.Hour(), t.Minute(), t.Second(), 0, loc)
	}
	return t
}
