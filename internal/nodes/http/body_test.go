package httpnodes

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/fsconfine"
	"github.com/monoes/mono-agent/internal/workflow"
)

func TestRequestBodyTypes(t *testing.T) {
	cases := []struct {
		name     string
		cfg      map[string]interface{}
		wantCT   string
		wantBody string
	}{
		{"json object", map[string]interface{}{"body_type": "json", "body": map[string]interface{}{"a": 1.0}}, "application/json", `{"a":1}`},
		{"json text", map[string]interface{}{"body_type": "json", "body": `{"b": [1, 2]}`}, "application/json", `{"b": [1, 2]}`},
		{"inferred json object", map[string]interface{}{"body": map[string]interface{}{"c": "x"}}, "application/json", `{"c":"x"}`},
		{"inferred json text", map[string]interface{}{"body": `[{"d":1}]`}, "application/json", `[{"d":1}]`},
		{"inferred text", map[string]interface{}{"body": "hello"}, "text/plain", "hello"},
		{"form object", map[string]interface{}{"body_type": "form", "body": map[string]interface{}{"b": "2", "a": "x y"}}, "application/x-www-form-urlencoded", "a=x+y&b=2"},
		{"form encoded text", map[string]interface{}{"body_type": "form", "body": "z=1&y=2"}, "application/x-www-form-urlencoded", "z=1&y=2"},
		{"form n8n rows", map[string]interface{}{"body_type": "form-urlencoded", "body": []interface{}{map[string]interface{}{"name": "k", "value": "v"}}}, "application/x-www-form-urlencoded", "k=v"},
		{"text", map[string]interface{}{"body_type": "text", "body": "plain"}, "text/plain", "plain"},
		{"raw", map[string]interface{}{"body_type": "raw", "raw_content_type": "application/xml", "body": "<a/>"}, "application/xml", "<a/>"},
		{"none", map[string]interface{}{"body_type": "none", "body": "ignored"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecorder(t, nil)
			tc.cfg["url"] = rec.URL
			tc.cfg["method"] = "POST"
			mustOK(t, &RequestNode{}, tc.cfg)
			got := rec.last(t)
			if ct := got.Header.Get("Content-Type"); ct != tc.wantCT {
				t.Errorf("Content-Type = %q, want %q", ct, tc.wantCT)
			}
			if got.Body != tc.wantBody {
				t.Errorf("body = %q, want %q", got.Body, tc.wantBody)
			}
		})
	}
}

// GET sends no inferred body; an explicit body_type still sends one.
func TestRequestGetSendsNoInferredBody(t *testing.T) {
	rec := newRecorder(t, nil)
	mustOK(t, &RequestNode{}, map[string]interface{}{"url": rec.URL, "body": `{"x":1}`})
	if b := rec.last(t).Body; b != "" {
		t.Fatalf("GET sent body %q", b)
	}
}

func TestRequestBinaryBody(t *testing.T) {
	rec := newRecorder(t, nil)
	item := workflow.Item{JSON: map[string]interface{}{}, Binary: map[string][]byte{"img": {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}}}
	mustOK(t, &RequestNode{}, map[string]interface{}{"url": rec.URL, "method": "PUT", "body_type": "binaryData", "binary_property": "img"}, item)
	got := rec.last(t)
	if got.Body != string(item.Binary["img"]) || got.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("body %q content-type %q", got.Body, got.Header.Get("Content-Type"))
	}

	_, e := execute(t, &RequestNode{}, map[string]interface{}{"url": rec.URL, "method": "PUT", "body_type": "binary"}, workflow.NewItem(map[string]interface{}{}))
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "binary_property") {
		t.Fatalf("missing binary data: %v", e)
	}
}

func readMultipart(t *testing.T, got seenRequest) map[string][2]string {
	t.Helper()
	_, params, err := mime.ParseMediaType(got.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	r := multipart.NewReader(strings.NewReader(got.Body), params["boundary"])
	parts := map[string][2]string{}
	for {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		parts[p.FormName()] = [2]string{p.FileName(), string(b)}
	}
	return parts
}

func TestRequestMultipartBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := newRecorder(t, nil)
	item := workflow.Item{JSON: map[string]interface{}{"who": "ann"}, Binary: map[string][]byte{"data": []byte("BIN")}}
	mustOK(t, &RequestNode{}, map[string]interface{}{
		"url": rec.URL, "method": "POST", "body_type": "multipart-form-data",
		"body": map[string]interface{}{
			"note":   "hi",
			"report": map[string]interface{}{"file": path},
			"blob":   map[string]interface{}{"binary_property": "data", "filename": "b.bin"},
		},
	}, item)
	parts := readMultipart(t, rec.last(t))
	if parts["note"] != [2]string{"", "hi"} || parts["report"] != [2]string{"report.csv", "a,b\n1,2\n"} || parts["blob"] != [2]string{"b.bin", "BIN"} {
		t.Fatalf("parts = %v", parts)
	}

	// n8n rows: formBinaryData + inputDataFieldName.
	mustOK(t, &RequestNode{}, map[string]interface{}{
		"url": rec.URL, "method": "POST", "body_type": "multipart",
		"body": []interface{}{
			map[string]interface{}{"name": "f", "parameter_type": "formBinaryData", "input_data_field_name": "data"},
			map[string]interface{}{"name": "t", "value": "v"},
		},
	}, item)
	parts = readMultipart(t, rec.last(t))
	if parts["f"][1] != "BIN" || parts["t"][1] != "v" {
		t.Fatalf("n8n rows: %v", parts)
	}
}

// A multipart file outside the org workdir is refused in a confined run.
func TestRequestMultipartFileIsConfined(t *testing.T) {
	root := t.TempDir()
	rec := newRecorder(t, nil)
	ctx := fsconfine.WithRoot(context.Background(), root)
	_, e := executeCtx(t, ctx, &RequestNode{}, map[string]interface{}{
		"url": rec.URL, "method": "POST", "body_type": "multipart",
		"body": map[string]interface{}{"f": map[string]interface{}{"file": "/etc/hostname"}},
	})
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "outside") || len(rec.all()) != 0 {
		t.Fatalf("want a confinement error and no request, got %v", e)
	}
}

func TestRequestQueryAndHeaderShapes(t *testing.T) {
	rec := newRecorder(t, nil)
	for _, tc := range []struct {
		format string
		want   map[string][]string
	}{
		{"repeat", map[string][]string{"tag": {"a", "b"}}},
		{"brackets", map[string][]string{"tag[]": {"a", "b"}}},
		{"indices", map[string][]string{"tag[0]": {"a"}, "tag[1]": {"b"}}},
	} {
		mustOK(t, &RequestNode{}, map[string]interface{}{
			"url": rec.URL + "/q?keep=1&limit=9", "query_array_format": tc.format,
			"query_params": map[string]interface{}{"tag": []interface{}{"a", "b"}, "limit": 5.0},
		})
		got := rec.last(t).Query
		for k, v := range tc.want {
			if strings.Join(got[k], ",") != strings.Join(v, ",") {
				t.Errorf("%s: %s = %v, want %v", tc.format, k, got[k], v)
			}
		}
		if got["keep"][0] != "1" || got["limit"][0] != "5" {
			t.Errorf("%s: URL params not kept/replaced: %v", tc.format, got)
		}
	}

	// n8n key/value rows and JSON text for headers and query.
	mustOK(t, &RequestNode{}, map[string]interface{}{
		"url":          rec.URL,
		"query_params": `[{"name":"page","value":"2"}]`,
		"headers":      []interface{}{map[string]interface{}{"name": "X-One", "value": "1"}},
	})
	got := rec.last(t)
	if got.Query["page"][0] != "2" || got.Header.Get("X-One") != "1" {
		t.Fatalf("rows: query %v header %q", got.Query, got.Header.Get("X-One"))
	}
	mustOK(t, &RequestNode{}, map[string]interface{}{"url": rec.URL, "headers": `{"X-Two": "2", "Host": "api.example.test"}`})
	if got := rec.last(t); got.Header.Get("X-Two") != "2" || got.Host != "api.example.test" {
		t.Fatalf("JSON headers: %v host %q", got.Header, got.Host)
	}

	_, e := execute(t, &RequestNode{}, map[string]interface{}{"url": rec.URL, "headers": "not json"})
	if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "headers") {
		t.Fatalf("bad headers: %v", e)
	}
}

// A URL with no query settings is sent byte for byte (signed URLs).
func TestRequestKeepsURLQueryAsGiven(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { raw = r.URL.RawQuery }))
	defer srv.Close()
	mustOK(t, &RequestNode{}, map[string]interface{}{"url": srv.URL + "/s?b=2&a=1&sig=x%2Fy"})
	if raw != "b=2&a=1&sig=x%2Fy" {
		t.Fatalf("raw query = %q", raw)
	}
}
