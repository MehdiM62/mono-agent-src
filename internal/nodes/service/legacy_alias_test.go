package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/nodes/comm"
	"github.com/monoes/mono-agent/internal/workflow"
)

// recordedRequest is one request a legacyServer saw.
type recordedRequest struct {
	Method, Path, RawQuery, Auth, ContentType string
	Body                                      []byte
}

func (r recordedRequest) form(t *testing.T) url.Values {
	t.Helper()
	v, err := url.ParseQuery(string(r.Body))
	if err != nil {
		t.Fatalf("parsing form body %q: %v", r.Body, err)
	}
	return v
}

func (r recordedRequest) json(t *testing.T) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("parsing JSON body %q: %v", r.Body, err)
	}
	return m
}

// legacyServer records every request and answers by "METHOD /path" from
// replies (default "{}").
type legacyServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []recordedRequest
}

func newLegacyServer(t *testing.T, replies map[string]string) *legacyServer {
	t.Helper()
	ls := &legacyServer{}
	ls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ls.mu.Lock()
		ls.reqs = append(ls.reqs, recordedRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
		ls.mu.Unlock()
		reply, ok := replies[r.Method+" "+r.URL.Path]
		if !ok {
			reply = "{}"
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(ls.Close)
	return ls
}

// find returns the first recorded request for "METHOD /path".
func (ls *legacyServer) find(t *testing.T, methodPath string) recordedRequest {
	t.Helper()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for _, r := range ls.reqs {
		if r.Method+" "+r.Path == methodPath {
			return r
		}
	}
	t.Fatalf("no %s request; saw %d requests: %+v", methodPath, len(ls.reqs), ls.reqs)
	return recordedRequest{}
}

func runLegacy(t *testing.T, node workflow.NodeExecutor, config map[string]interface{}) []workflow.Item {
	t.Helper()
	out, err := node.Execute(context.Background(), workflow.NodeInput{}, config)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(out) != 1 || out[0].Handle != "main" {
		t.Fatalf("outputs = %+v, want one main output", out)
	}
	return out[0].Items
}

// TestLegacyCommTypesResolveToCanonical checks the registry wiring: every
// old comm.* type id still resolves to a runnable node, the canonical
// service.* one, and none of the old ids is listed by Types() (which feeds
// `node list` and the palette).
func TestLegacyCommTypesResolveToCanonical(t *testing.T) {
	r := workflow.NewNodeTypeRegistry()
	RegisterAll(r)
	comm.RegisterAll(r)

	cases := map[string]string{
		"comm.bluesky":      "service.bluesky",
		"comm.mastodon":     "service.mastodon",
		"comm.reddit":       "service.reddit",
		"comm.discord":      "service.discord",
		"comm.outlook_read": "service.outlook_mail",
		"comm.outlook_send": "service.outlook_mail",
	}
	listed := map[string]bool{}
	for _, typ := range r.Types() {
		listed[typ] = true
	}
	for legacy, canonical := range cases {
		f, ok := r.Get(legacy)
		if !ok {
			t.Errorf("%s: not resolvable", legacy)
			continue
		}
		if got := f().Type(); got != canonical {
			t.Errorf("%s resolves to %s, want %s", legacy, got, canonical)
		}
		if listed[legacy] {
			t.Errorf("%s is listed by Types(); only canonical types should be", legacy)
		}
		if !listed[canonical] {
			t.Errorf("%s is not listed by Types()", canonical)
		}
	}
}
