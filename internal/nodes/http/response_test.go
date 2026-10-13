package httpnodes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

func serve(t *testing.T, status int, contentType, body string, extra map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		for k, v := range extra {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/files/report.pdf"
}

func TestRequestResponseShapes(t *testing.T) {
	t.Run("body only object", func(t *testing.T) {
		m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": serve(t, 200, "application/json", `{"id":7}`, nil), "full_response": false})
		if len(m) != 1 || m[0].JSON["id"] != 7.0 {
			t.Fatalf("got %v", m)
		}
	})
	t.Run("body only list splits", func(t *testing.T) {
		m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": serve(t, 200, "application/json", `[{"id":1},{"id":2},3]`, nil), "full_response": "false"})
		if len(m) != 3 || m[1].JSON["id"] != 2.0 || m[2].JSON["data"] != 3.0 {
			t.Fatalf("got %v", m)
		}
	})
	t.Run("body only text under output_property", func(t *testing.T) {
		m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": serve(t, 200, "text/html", `<p>hi</p>`, nil), "full_response": false, "response_format": "text", "output_property": "html"})
		if m[0].JSON["html"] != "<p>hi</p>" {
			t.Fatalf("got %v", m[0].JSON)
		}
	})
	t.Run("autodetect json", func(t *testing.T) {
		m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": serve(t, 200, "application/problem+json", `{"a":1}`, nil), "response_format": "autodetect"})
		if b, ok := m[0].JSON["body"].(map[string]interface{}); !ok || b["a"] != 1.0 {
			t.Fatalf("got %v", m[0].JSON)
		}
	})
	t.Run("autodetect file keeps binary", func(t *testing.T) {
		m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": serve(t, 200, "application/pdf", "%PDF-1.4", nil), "response_format": "autodetect", "full_response": false})
		if string(m[0].Binary["data"]) != "%PDF-1.4" || m[0].JSON["file_name"] != "report.pdf" || m[0].JSON["mime_type"] != "application/pdf" || m[0].JSON["file_size"] != 8 {
			t.Fatalf("got %v %q", m[0].JSON, m[0].Binary["data"])
		}
	})
	t.Run("file name from content-disposition", func(t *testing.T) {
		m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": serve(t, 200, "text/csv", "a,b", map[string]string{"Content-Disposition": `attachment; filename="out.csv"`}), "response_format": "file", "output_property": "csv"})
		body := m[0].JSON["body"].(map[string]interface{})
		if body["file_name"] != "out.csv" || string(m[0].Binary["csv"]) != "a,b" {
			t.Fatalf("got %v", m[0].JSON)
		}
	})
	t.Run("binary is base64", func(t *testing.T) {
		m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": serve(t, 200, "", "AB", nil), "response_format": "binary"})
		if m[0].JSON["body"] != "QUI=" {
			t.Fatalf("got %v", m[0].JSON["body"])
		}
	})
	t.Run("lowercase headers", func(t *testing.T) {
		m := mustOK(t, &RequestNode{}, map[string]interface{}{"url": serve(t, 200, "text/plain", "x", map[string]string{"X-Rate-Limit": "9"}), "lowercase_headers": true})
		if m[0].JSON["headers"].(map[string]interface{})["x-rate-limit"] != "9" {
			t.Fatalf("got %v", m[0].JSON["headers"])
		}
	})
}

func TestRequestNonSuccessAndNeverError(t *testing.T) {
	u := serve(t, 404, "application/json", `{"message":"nope"}`, nil)
	m, e := execute(t, &RequestNode{}, map[string]interface{}{"url": u, "full_response": false})
	if len(m) != 0 || len(e) != 1 || e[0].JSON["status"] != 404 {
		t.Fatalf("404 should reach the error handle with its status: %v %v", m, e)
	}
	m, e = execute(t, &RequestNode{}, map[string]interface{}{"url": u, "never_error": true, "full_response": false})
	if len(e) != 0 || m[0].JSON["message"] != "nope" {
		t.Fatalf("never_error: %v %v", m, e)
	}
}

func TestRequestClientOptions(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
		defer srv.Close()
		_, e := execute(t, &RequestNode{}, map[string]interface{}{"url": srv.URL, "timeout": 0.05})
		if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "Timeout") {
			t.Fatalf("want a timeout error, got %v", e)
		}
	})
	t.Run("bad timeout names the field", func(t *testing.T) {
		_, err := (&RequestNode{}).Execute(t.Context(), workflow.NodeInput{}, map[string]interface{}{"url": "http://x", "timeout": "soon"})
		if err == nil || !strings.Contains(err.Error(), "timeout") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("tls certificates", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) }))
		defer srv.Close()
		if _, e := execute(t, &RequestNode{}, map[string]interface{}{"url": srv.URL}); len(e) != 1 {
			t.Fatal("a self-signed certificate was accepted by default")
		}
		mustOK(t, &RequestNode{}, map[string]interface{}{"url": srv.URL, "allow_unauthorized_certs": true})
	})
	t.Run("proxy", func(t *testing.T) {
		var proxied string
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxied = r.URL.String() }))
		defer proxy.Close()
		mustOK(t, &RequestNode{}, map[string]interface{}{"url": "http://upstream.invalid/x?y=1", "proxy": proxy.URL})
		if proxied != "http://upstream.invalid/x?y=1" {
			t.Fatalf("proxy saw %q", proxied)
		}
		if _, err := (&RequestNode{}).Execute(t.Context(), workflow.NodeInput{}, map[string]interface{}{"url": "http://x", "proxy": "::"}); err == nil {
			t.Fatal("a bad proxy URL was accepted")
		}
	})
	t.Run("max_redirects", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, srv.URL+"/again", http.StatusFound)
		}))
		defer srv.Close()
		_, e := execute(t, &RequestNode{}, map[string]interface{}{"url": srv.URL, "max_redirects": 2})
		if len(e) != 1 || !strings.Contains(e[0].JSON["error"].(string), "stopped after 2 redirects") {
			t.Fatalf("got %v", e)
		}
	})
	t.Run("batching pauses between batches", func(t *testing.T) {
		rec := newRecorder(t, nil)
		items := []workflow.Item{workflow.NewItem(map[string]interface{}{"n": 1.0}), workflow.NewItem(map[string]interface{}{"n": 2.0}), workflow.NewItem(map[string]interface{}{"n": 3.0})}
		start := time.Now()
		mustOK(t, &RequestNode{}, map[string]interface{}{"url": rec.URL + "/i/{{ $json.n }}", "batch_size": 1, "batch_interval_ms": 60}, items...)
		if d := time.Since(start); d < 120*time.Millisecond {
			t.Fatalf("3 items in batches of 1 took %v, want two 60ms pauses", d)
		}
		if reqs := rec.all(); len(reqs) != 3 || reqs[2].Path != "/i/3" {
			t.Fatalf("requests: %v", reqs)
		}
	})
	t.Run("bad method", func(t *testing.T) {
		if _, err := (&RequestNode{}).Execute(t.Context(), workflow.NodeInput{}, map[string]interface{}{"url": "http://x", "method": "GE T"}); err == nil {
			t.Fatal("a bad method was accepted")
		}
	})
}
