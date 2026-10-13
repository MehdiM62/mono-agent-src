package data

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

func dtRun(t *testing.T, in []workflow.Item, cfg map[string]interface{}) []map[string]interface{} {
	t.Helper()
	if _, ok := cfg["timezone"]; !ok {
		cfg["timezone"] = "UTC"
	}
	return run(t, &DateTimeNode{}, in, cfg)
}

func TestDateTimeV2AddSubtract(t *testing.T) {
	in := items(t, `[{"d":"2024-01-31T10:00:00Z"},{"d":"2024-03-15 08:30:00"}]`)
	got := dtRun(t, in, map[string]interface{}{"operation": "addToDate", "magnitude": "{{ $json.d }}", "time_unit": "months", "duration": 1.0})
	if got[0]["newDate"] != "2024-02-29T10:00:00.000Z" || got[1]["newDate"] != "2024-04-15T08:30:00.000Z" {
		t.Errorf("add months: %v", got)
	}
	if _, ok := got[0]["d"]; ok {
		t.Error("include_input_fields defaults to false")
	}
	got = dtRun(t, in, map[string]interface{}{"operation": "subtract_from_date", "magnitude": "{{ $json.d }}", "time_unit": "hours", "duration": "1.5", "include_input_fields": true, "output_field_name": "earlier"})
	if got[0]["earlier"] != "2024-01-31T08:30:00.000Z" || got[0]["d"] == nil {
		t.Errorf("subtract hours: %v", got)
	}
	for _, unit := range []string{"years", "quarters", "weeks", "days", "minutes", "seconds", "milliseconds"} {
		if _, err := (&DateTimeNode{}).Execute(context.Background(), workflow.NodeInput{Items: in}, map[string]interface{}{"operation": "add_to_date", "magnitude": "2024-01-01", "time_unit": unit, "duration": 2.0}); err != nil {
			t.Errorf("unit %s: %v", unit, err)
		}
	}
	if _, err := (&DateTimeNode{}).Execute(context.Background(), workflow.NodeInput{Items: in}, map[string]interface{}{"operation": "add_to_date", "magnitude": "2024-01-01", "time_unit": "fortnights", "duration": 2.0}); err == nil {
		t.Error("unknown unit: expected error")
	}
}

func TestDateTimeV2FormatDate(t *testing.T) {
	in := items(t, `[{"d":"2024-07-04T15:04:05.123Z","ts":1720105445,"ms":1720105445123,"custom":"20240704"}]`)
	cases := []struct {
		cfg  map[string]interface{}
		want string
	}{
		{map[string]interface{}{"date": "{{ $json.d }}"}, "07/04/2024"},
		{map[string]interface{}{"date": "{{ $json.d }}", "format": "custom", "custom_format": "yyyy-MM-dd'T'HH:mm:ss.SSS ZZ EEEE MMMM o q"}, "2024-07-04T15:04:05.123 +00:00 Thursday July 186 3"},
		{map[string]interface{}{"date": "{{ $json.ts }}", "format": "X"}, "1720105445"},
		{map[string]interface{}{"date": "{{ $json.ms }}", "format": "x"}, "1720105445123"},
		{map[string]interface{}{"date": "{{ $json.custom }}", "from_format": "yyyyMMdd", "format": "MMMM dd yyyy"}, "July 04 2024"},
		{map[string]interface{}{"date": "{{ $json.d }}", "format": "custom", "custom_format": "h:mm a", "timezone": "America/New_York"}, "11:04 AM"},
	}
	for _, c := range cases {
		c.cfg["operation"] = "formatDate"
		got := dtRun(t, in, c.cfg)
		if got[0]["formattedDate"] != c.want {
			t.Errorf("%v: got %v, want %q", c.cfg, got[0]["formattedDate"], c.want)
		}
	}
	if _, err := (&DateTimeNode{}).Execute(context.Background(), workflow.NodeInput{Items: in}, map[string]interface{}{"operation": "format_date", "date": "not a date", "timezone": "UTC"}); err == nil || !strings.Contains(err.Error(), "date") {
		t.Errorf("invalid date: err = %v", err)
	}
	if _, err := (&DateTimeNode{}).Execute(context.Background(), workflow.NodeInput{Items: in}, map[string]interface{}{"operation": "format_date", "date": "2024-01-01", "timezone": "Mars/Base"}); err == nil {
		t.Error("bad timezone: expected error")
	}
}

func TestDateTimeV2RoundExtractCurrent(t *testing.T) {
	in := items(t, `[{"d":"2024-05-15T13:45:30Z"}]`)
	for cfg, want := range map[[3]string]string{
		{"round_down", "to_nearest", "month"}: "2024-05-01T00:00:00.000Z",
		{"round_down", "to_nearest", "week"}:  "2024-05-13T00:00:00.000Z",
		{"round_down", "to_nearest", "hour"}:  "2024-05-15T13:00:00.000Z",
		{"roundUp", "to", "month"}:            "2024-06-01T00:00:00.000Z",
		{"round_up", "to", "year"}:            "2025-01-01T00:00:00.000Z",
	} {
		got := dtRun(t, in, map[string]interface{}{"operation": "roundDate", "date": "{{ $json.d }}", "mode": cfg[0], cfg[1]: cfg[2]})
		if got[0]["roundedDate"] != want {
			t.Errorf("%v: got %v, want %s", cfg, got[0]["roundedDate"], want)
		}
	}
	for part, want := range map[string]int{"year": 2024, "month": 5, "week": 20, "day": 15, "hour": 13, "minute": 45, "second": 30} {
		got := dtRun(t, in, map[string]interface{}{"operation": "extract_date", "date": "{{ $json.d }}", "part": part})
		if got[0]["datePart"] != want {
			t.Errorf("part %s: got %v want %d", part, got[0]["datePart"], want)
		}
	}
	got := dtRun(t, in, map[string]interface{}{"operation": "getCurrentDate", "include_time": false})
	s, _ := got[0]["currentDate"].(string)
	if !strings.HasSuffix(s, "T00:00:00.000Z") || !strings.HasPrefix(s, time.Now().UTC().Format("2006-01-02")) {
		t.Errorf("current date: %q", s)
	}
}

func TestDateTimeV2TimeBetween(t *testing.T) {
	in := items(t, `[{"a":"2024-01-01T00:00:00Z","b":"2024-03-02T12:00:00Z"}]`)
	cfg := map[string]interface{}{"operation": "get_time_between_dates", "start_date": "{{ $json.a }}", "end_date": "{{ $json.b }}"}
	got := dtRun(t, in, cfg)
	wantJSON(t, got[0]["timeDifference"], `{"days":61.5}`)

	cfg["units"] = []interface{}{"month", "day", "hour"}
	got = dtRun(t, in, cfg)
	wantJSON(t, got[0]["timeDifference"], `{"months":2,"days":1,"hours":12}`)

	cfg["iso_string"] = true
	got = dtRun(t, in, cfg)
	if got[0]["timeDifference"] != "P2M1DT12H" {
		t.Errorf("iso: %v", got[0]["timeDifference"])
	}

	rev := map[string]interface{}{"operation": "get_time_between_dates", "start_date": "{{ $json.b }}", "end_date": "{{ $json.a }}", "units": "hour"}
	got = dtRun(t, in, rev)
	wantJSON(t, got[0]["timeDifference"], `{"hours":-1476}`)
}

func TestLuxonToGoLayout(t *testing.T) {
	if l, err := luxonToGoLayout("dd.MM.yyyy HH:mm"); err != nil || l != "02.01.2006 15:04" {
		t.Errorf("got %q, %v", l, err)
	}
	if _, err := luxonToGoLayout("kkkk"); err == nil {
		t.Error("unsupported token: expected error")
	}
}

func TestParseFlexibleDateTimestamps(t *testing.T) {
	for raw, want := range map[interface{}]int64{
		1700000000.0:    1700000000,
		"1700000000":    1700000000,
		1700000000123.0: 1700000000,
		"1700000000.5":  1700000000,
	} {
		got, err := parseFlexibleDate(raw, "", time.UTC)
		if err != nil || got.Unix() != want {
			t.Errorf("%v: got %v (%v)", raw, got, err)
		}
	}
}
