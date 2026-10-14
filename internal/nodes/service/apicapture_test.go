package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// capturedReq is one request an apiCapture server received.
type capturedReq struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// form parses the body as application/x-www-form-urlencoded.
func (r capturedReq) form(t *testing.T) url.Values {
	t.Helper()
	v, err := url.ParseQuery(string(r.Body))
	if err != nil {
		t.Fatalf("parsing form body %q: %v", r.Body, err)
	}
	return v
}

// json decodes the body as a JSON object.
func (r capturedReq) json(t *testing.T) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("decoding JSON body %q: %v", r.Body, err)
	}
	return m
}

// apiCapture is an httptest server that records every request and answers
// with whatever respond returns (200 "{}" when respond is nil).
type apiCapture struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []capturedReq
}

func newAPICapture(t *testing.T, respond func(r *http.Request) (int, string)) *apiCapture {
	t.Helper()
	c := &apiCapture{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.reqs = append(c.reqs, capturedReq{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body,
		})
		c.mu.Unlock()
		status, out := http.StatusOK, "{}"
		if respond != nil {
			status, out = respond(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, out)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

// point sets *ptr to the server URL plus suffix for the duration of the test.
func (c *apiCapture) point(t *testing.T, ptr *string, suffix string) {
	t.Helper()
	orig := *ptr
	*ptr = c.srv.URL + suffix
	t.Cleanup(func() { *ptr = orig })
}

func (c *apiCapture) all() []capturedReq {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedReq(nil), c.reqs...)
}

// only asserts exactly one request was received and returns it.
func (c *apiCapture) only(t *testing.T) capturedReq {
	t.Helper()
	reqs := c.all()
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1: %+v", len(reqs), reqs)
	}
	return reqs[0]
}

// capRun executes node and returns its main-handle items, failing on error.
func capRun(t *testing.T, node workflow.NodeExecutor, input workflow.NodeInput, config map[string]interface{}) []workflow.Item {
	t.Helper()
	out, err := node.Execute(context.Background(), input, config)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, o := range out {
		if o.Handle == "main" {
			return o.Items
		}
	}
	return nil
}

// capRunErr executes node and returns its error, failing if there was none.
func capRunErr(t *testing.T, node workflow.NodeExecutor, config map[string]interface{}) error {
	t.Helper()
	_, err := node.Execute(context.Background(), workflow.NodeInput{}, config)
	if err == nil {
		t.Fatalf("Execute(%v): want error, got nil", config)
	}
	return err
}

// capWantValues checks that every key in want has exactly that value in got.
func capWantValues(t *testing.T, what string, got url.Values, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if g := got.Get(k); g != v {
			t.Errorf("%s %s = %q, want %q (all: %v)", what, k, g, v, got)
		}
	}
}

// capWantErr fails unless err's message contains every fragment.
func capWantErr(t *testing.T, err error, fragments ...string) {
	t.Helper()
	for _, f := range fragments {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("error %q does not mention %q", err, f)
		}
	}
}

// capMerge returns base with extra's keys added (extra wins).
func capMerge(base, extra map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
