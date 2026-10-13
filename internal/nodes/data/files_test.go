package data

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/fsconfine"
	"github.com/monoes/mono-agent/internal/workflow"
)

func execItems(t *testing.T, n workflow.NodeExecutor, ctx context.Context, in []workflow.Item, cfg map[string]interface{}) []workflow.Item {
	t.Helper()
	out, err := n.Execute(ctx, workflow.NodeInput{Items: in}, cfg)
	if err != nil {
		t.Fatalf("%s %v: %v", n.Type(), cfg["operation"], err)
	}
	return out[0].Items
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

const tableRows = `[{"name":"a","n":1,"o":{"x":true}},{"name":"b,c","n":2.5}]`

func TestConvertExtractTableRoundTrips(t *testing.T) {
	ctx := context.Background()
	for _, op := range []string{"csv", "xlsx", "ods", "html", "rtf"} {
		files := execItems(t, &ConvertToFileNode{}, ctx, items(t, tableRows), map[string]interface{}{"operation": op, "file_name": "report." + op})
		if len(files) != 1 || files[0].JSON["file_name"] != "report."+op || files[0].JSON["mime_type"] != spreadsheetMime[op] {
			t.Fatalf("%s: file item %v", op, files[0].JSON)
		}
		back := execItems(t, &ExtractFromFileNode{}, ctx, files, map[string]interface{}{"operation": op})
		if len(back) != 2 || back[1].JSON["name"] != "b,c" || back[0].JSON["o.x"] == nil {
			t.Errorf("%s round trip: %v", op, asJSON(back))
		}
		if n := back[1].JSON["n"]; n != 2.5 && n != "2.5" {
			t.Errorf("%s number: %v", op, n)
		}
	}
	files := execItems(t, &ConvertToFileNode{}, ctx, items(t, tableRows), map[string]interface{}{"operation": "csv", "delimiter": ";", "header_row": false})
	raw, _ := base64.StdEncoding.DecodeString(files[0].JSON["data"].(string))
	if !strings.HasPrefix(string(raw), "1;a;true\n") {
		t.Errorf("csv options: %q", raw)
	}
	for _, op := range []string{"xls"} {
		if _, err := (&ConvertToFileNode{}).Execute(ctx, workflow.NodeInput{Items: items(t, tableRows)}, map[string]interface{}{"operation": op}); err == nil {
			t.Errorf("convert %s: expected not-supported error", op)
		}
		if _, err := (&ExtractFromFileNode{}).Execute(ctx, workflow.NodeInput{Items: files}, map[string]interface{}{"operation": op}); err == nil {
			t.Errorf("extract %s: expected not-supported error", op)
		}
	}
}

func TestConvertExtractJSONTextBinary(t *testing.T) {
	ctx := context.Background()
	once := execItems(t, &ConvertToFileNode{}, ctx, items(t, tableRows), map[string]interface{}{"operation": "json"})
	back := execItems(t, &ExtractFromFileNode{}, ctx, once, map[string]interface{}{"operation": "json", "destination_key": "rows"})
	wantJSON(t, back[0].JSON["rows"], `[{"n":1,"name":"a","o":{"x":true}},{"n":2.5,"name":"b,c"}]`)

	each := execItems(t, &ConvertToFileNode{}, ctx, items(t, tableRows), map[string]interface{}{"operation": "json", "mode": "each", "format": true})
	if len(each) != 2 {
		t.Fatalf("json each: %d items", len(each))
	}

	text := execItems(t, &ConvertToFileNode{}, ctx, items(t, `[{"s":"héllo"}]`), map[string]interface{}{"operation": "text", "source_property": "s", "encoding": "utf16le", "add_bom": true})
	got := execItems(t, &ExtractFromFileNode{}, ctx, text, map[string]interface{}{"operation": "text", "encoding": "utf16le", "destination_key": "t"})
	if got[0].JSON["t"] != "héllo" {
		t.Errorf("utf16 text round trip: %v", got[0].JSON)
	}

	bin := execItems(t, &ConvertToFileNode{}, ctx, items(t, `[{"b":"`+b64("<x/>")+`"}]`), map[string]interface{}{"operation": "to_binary", "source_property": "b", "mime_type": "application/xml", "file_name": "x.xml"})
	if bin[0].JSON["mime_type"] != "application/xml" || bin[0].JSON["data"] != b64("<x/>") {
		t.Errorf("to_binary: %v", bin[0].JSON)
	}
	xml := execItems(t, &ExtractFromFileNode{}, ctx, bin, map[string]interface{}{"operation": "xml"})
	if xml[0].JSON["data"] != "<x/>" {
		t.Errorf("extract xml: %v", xml[0].JSON)
	}
	prop := execItems(t, &ExtractFromFileNode{}, ctx, bin, map[string]interface{}{"operation": "binary_to_property", "destination_key": "b64", "keep_source": "both"})
	if prop[0].JSON["b64"] != b64("<x/>") || prop[0].JSON["data"] == nil {
		t.Errorf("binary_to_property keep both: %v", prop[0].JSON)
	}
	if _, err := (&ExtractFromFileNode{}).Execute(ctx, workflow.NodeInput{Items: items(t, `[{"other":1}]`)}, map[string]interface{}{"operation": "text"}); err == nil {
		t.Error("missing binary: expected error")
	}
}

func TestICalAndICSRoundTrip(t *testing.T) {
	ctx := context.Background()
	ev := execItems(t, &ICalNode{}, ctx, items(t, `[{"t":"Standup"}]`), map[string]interface{}{
		"title": "{{ $json.t }}", "start": "2026-10-14T10:00:00Z", "end": "2026-10-14T10:15:00Z",
		"description": "Line one, with; specials\nline two", "location": "Room 1", "uid": "u-1",
		"attendees": `[{"name":"Ada","email":"ada@example.com","rsvp":true}]`,
		"organizer": map[string]interface{}{"name": "Bob", "email": "bob@example.com"},
		"status": "CONFIRMED", "recurrence_rule": "FREQ=WEEKLY;COUNT=3", "file_name": "standup.ics",
	})
	raw, _ := base64.StdEncoding.DecodeString(ev[0].JSON["data"].(string))
	ics := string(raw)
	for _, want := range []string{"BEGIN:VCALENDAR\r\n", "SUMMARY:Standup\r\n", "DTSTART:20261014T100000Z\r\n", "UID:u-1\r\n", "RRULE:FREQ=WEEKLY;COUNT=3\r\n", `Line one\, with\; specials\nline two`, "mailto:ada@example.com", "END:VCALENDAR\r\n"} {
		if !strings.Contains(ics, want) {
			t.Errorf("ics missing %q:\n%s", want, ics)
		}
	}
	for _, line := range strings.Split(ics, "\r\n") {
		if len(line) > 75 {
			t.Errorf("line not folded (%d octets): %q", len(line), line)
		}
	}
	if ev[0].JSON["file_name"] != "standup.ics" || ev[0].JSON["mime_type"] != "text/calendar" {
		t.Errorf("meta: %v", ev[0].JSON)
	}
	parsed := execItems(t, &ExtractFromFileNode{}, ctx, ev, map[string]interface{}{"operation": "ics"})
	evs := parsed[0].JSON["data"].(map[string]interface{})["events"].([]interface{})
	if evs[0].(map[string]interface{})["summary"] != "Standup" || evs[0].(map[string]interface{})["description"] != "Line one, with; specials\nline two" {
		t.Errorf("parsed: %v", asJSON(evs))
	}
	allDay := execItems(t, &ConvertToFileNode{}, ctx, items(t, `[{}]`), map[string]interface{}{"operation": "ics", "title": "Day", "start": "2026-10-14", "all_day": true})
	raw, _ = base64.StdEncoding.DecodeString(allDay[0].JSON["data"].(string))
	if !strings.Contains(string(raw), "DTSTART;VALUE=DATE:20261014") || !strings.Contains(string(raw), "DTEND;VALUE=DATE:20261015") {
		t.Errorf("all day: %s", raw)
	}
	if _, err := (&ICalNode{}).Execute(ctx, workflow.NodeInput{Items: items(t, `[{}]`)}, map[string]interface{}{"title": "x", "start": "not a date"}); err == nil {
		t.Error("bad start: expected error")
	}
}

func TestExtractPDF(t *testing.T) {
	pdf, err := os.ReadFile("testdata/files/two_pages.pdf")
	if err != nil {
		t.Fatal(err)
	}
	in := []workflow.Item{{JSON: map[string]interface{}{"data": base64.StdEncoding.EncodeToString(pdf), "keep": 1.0}}}
	got := execItems(t, &ExtractFromFileNode{}, context.Background(), in, map[string]interface{}{"operation": "pdf"})
	if got[0].JSON["text"] != "Hello page one\n\nSecond page text" || got[0].JSON["numpages"] != 2 {
		t.Errorf("pdf: %v", got[0].JSON)
	}
	got = execItems(t, &ExtractFromFileNode{}, context.Background(), in, map[string]interface{}{"operation": "pdf", "join_pages": false, "max_pages": 1.0})
	wantJSON(t, got[0].JSON["text"], `["Hello page one"]`)
	in[0].JSON["data"] = b64("not a pdf")
	if _, err := (&ExtractFromFileNode{}).Execute(context.Background(), workflow.NodeInput{Items: in}, map[string]interface{}{"operation": "pdf"}); err == nil {
		t.Error("garbage pdf: expected error")
	}
}

func TestCompressionCompressDecompress(t *testing.T) {
	ctx := context.Background()
	in := []workflow.Item{{JSON: map[string]interface{}{
		"a": b64("AAA"), "b": b64("BB"),
		"files": []interface{}{
			map[string]interface{}{"property": "a", "file_name": "a.txt"},
			map[string]interface{}{"property": "b", "file_name": "b.txt"},
		},
	}}}
	for _, format := range []string{"zip", "tar", "targz"} {
		packed := execItems(t, &CompressionNode{}, ctx, in, map[string]interface{}{"operation": "compress", "binary_property_name": "a,b", "output_format": format, "binary_property_output": "arc"})
		if packed[0].JSON["arc"] == nil {
			t.Fatalf("%s: no archive: %v", format, packed[0].JSON)
		}
		un := execItems(t, &CompressionNode{}, ctx, packed, map[string]interface{}{"operation": "decompress", "binary_property_name": "arc", "output_prefix": "f"})
		j := un[0].JSON
		if j["f0"] != b64("AAA") || j["f1"] != b64("BB") {
			t.Errorf("%s round trip: %v", format, asJSON(j))
		}
		if files := j["files"].([]interface{}); files[0].(map[string]interface{})["file_name"] != "a.txt" {
			t.Errorf("%s names: %v", format, files)
		}
	}
	gz := execItems(t, &CompressionNode{}, ctx, in, map[string]interface{}{"operation": "compress", "binary_property_name": "a, b", "output_format": "gzip"})
	un := execItems(t, &CompressionNode{}, ctx, gz, map[string]interface{}{"operation": "decompress", "binary_property_name": "data,data1"})
	if un[0].JSON["file_0"] != b64("AAA") || un[0].JSON["file_1"] != b64("BB") {
		t.Errorf("gzip: in %v out %v", asJSON(gz[0].JSON), asJSON(un[0].JSON))
	}
	if _, err := (&CompressionNode{}).Execute(ctx, workflow.NodeInput{Items: in}, map[string]interface{}{"operation": "compress", "binary_property_name": "missing"}); err == nil {
		t.Error("missing property: expected error")
	}
}

func TestReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	node := &ReadWriteFileNode{}
	write := func(name, content string, appendMode bool) {
		execItems(t, node, ctx, []workflow.Item{{JSON: map[string]interface{}{"data": b64(content)}}}, map[string]interface{}{"operation": "write", "file_name": filepath.Join(dir, name), "append": appendMode})
	}
	write("a.txt", "one", false)
	write("a.txt", "+two", true)
	write("sub/deep/b.txt", "bee", false)
	write("c.csv", "x", false)
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "one+two" {
		t.Errorf("append: %q", b)
	}
	got := execItems(t, node, ctx, []workflow.Item{{}}, map[string]interface{}{"operation": "read", "file_selector": filepath.Join(dir, "*.txt")})
	if len(got) != 1 || got[0].JSON["file_name"] != "a.txt" || got[0].JSON["data"] != b64("one+two") || got[0].JSON["mime_type"] != "text/plain" {
		t.Errorf("glob read: %v", asJSON(got))
	}
	got = execItems(t, node, ctx, []workflow.Item{{}}, map[string]interface{}{"operation": "read", "file_selector": filepath.Join(dir, "**", "*.txt"), "data_property_name": "file"})
	if len(got) != 2 || got[1].JSON["file"] != b64("bee") {
		t.Errorf("** read: %v", asJSON(got))
	}
	if _, err := node.Execute(ctx, workflow.NodeInput{Items: []workflow.Item{{}}}, map[string]interface{}{"operation": "read", "file_selector": filepath.Join(dir, "*.none")}); err == nil {
		t.Error("no match: expected error")
	}
}

func TestReadWriteFileConfinedToOrgWorkdir(t *testing.T) {
	ctx, root, outside := confinedDirs(t)
	node := &ReadWriteFileNode{}
	for _, sel := range []string{filepath.Join(outside, "*.csv"), "../outside/*.csv", "escape/*.csv"} {
		_, err := node.Execute(ctx, workflow.NodeInput{Items: []workflow.Item{{}}}, map[string]interface{}{"operation": "read", "file_selector": sel})
		if err == nil {
			t.Errorf("read %q: expected refusal", sel)
		}
	}
	in := []workflow.Item{{JSON: map[string]interface{}{"data": b64("x")}}}
	for _, p := range []string{filepath.Join(outside, "w.txt"), "../outside/w.txt", "escape/w.txt"} {
		_, err := node.Execute(ctx, workflow.NodeInput{Items: in}, map[string]interface{}{"operation": "write", "file_name": p})
		if !errors.Is(err, fsconfine.ErrOutsideWorkdir) {
			t.Errorf("write %q: err = %v, want ErrOutsideWorkdir", p, err)
		}
	}
	execItems(t, node, ctx, in, map[string]interface{}{"operation": "write", "file_name": "ok/w.txt"})
	if _, err := os.Stat(filepath.Join(root, "ok", "w.txt")); err != nil {
		t.Errorf("relative write inside the workdir: %v", err)
	}
}
