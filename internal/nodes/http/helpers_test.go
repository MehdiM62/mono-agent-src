package httpnodes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// seenRequest is what a test server received.
type seenRequest struct {
	Method string
	Path   string
	Query  map[string][]string
	Header http.Header
	Body   string
	Host   string
}

// recorder is an httptest server that records every request and answers
// with respond (200 {"ok":true} when nil).
type recorder struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []seenRequest
}

func newRecorder(t *testing.T, respond http.HandlerFunc) *recorder {
	t.Helper()
	rec := &recorder{}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, seenRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: string(b), Host: r.Host})
		rec.mu.Unlock()
		if respond != nil {
			respond(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (r *recorder) last(t *testing.T) seenRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reqs) == 0 {
		t.Fatal("the server received no request")
	}
	return r.reqs[len(r.reqs)-1]
}

func (r *recorder) all() []seenRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]seenRequest(nil), r.reqs...)
}

// execute runs node with cfg over items and returns main and error items.
func execute(t *testing.T, node workflow.NodeExecutor, cfg map[string]interface{}, items ...workflow.Item) (mainItems, errItems []workflow.Item) {
	t.Helper()
	return executeCtx(t, context.Background(), node, cfg, items...)
}

func executeCtx(t *testing.T, ctx context.Context, node workflow.NodeExecutor, cfg map[string]interface{}, items ...workflow.Item) (mainItems, errItems []workflow.Item) {
	t.Helper()
	out, err := node.Execute(ctx, workflow.NodeInput{Items: items}, cfg)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return handleItems(out, "main"), handleItems(out, "error")
}

// mustOK runs node and fails the test when anything reached the error handle.
func mustOK(t *testing.T, node workflow.NodeExecutor, cfg map[string]interface{}, items ...workflow.Item) []workflow.Item {
	t.Helper()
	m, e := execute(t, node, cfg, items...)
	if len(e) > 0 {
		t.Fatalf("error items: %v", e[0].JSON)
	}
	return m
}
