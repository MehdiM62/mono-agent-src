package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

func newStateContext(t *testing.T) context.Context {
	t.Helper()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return vault.ContextWithDB(context.Background(), db.DB)
}

var stateInput = workflow.NodeInput{WorkflowID: "wf1", NodeID: "n1", ExecutionID: "e"}

// fakeGmail lists its messages newest first, honouring an "after:<epoch s>"
// term of q the way Gmail does, and serves each message's internalDate.
type fakeGmail struct {
	mu       sync.Mutex
	msgs     map[string]int64 // id -> internalDate (ms)
	queries  []string
	failMeta bool
}

func (f *fakeGmail) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "no auth", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/messages" {
		q := r.URL.Query().Get("q")
		f.queries = append(f.queries, q)
		after := int64(-1 << 62)
		for _, term := range strings.Fields(q) {
			if v, ok := strings.CutPrefix(term, "after:"); ok {
				fmt.Sscan(v, &after)
			}
		}
		var ids []string
		for id, ms := range f.msgs {
			if ms/1000 > after {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return f.msgs[ids[i]] > f.msgs[ids[j]] })
		list := make([]map[string]string, len(ids))
		for i, id := range ids {
			list[i] = map[string]string{"id": id, "threadId": "t" + id}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"messages": list})
		return
	}
	id, ok := strings.CutPrefix(r.URL.Path, "/messages/")
	if !ok || r.URL.Query().Get("format") != "minimal" {
		http.NotFound(w, r)
		return
	}
	if f.failMeta {
		http.Error(w, "backend error", http.StatusInternalServerError)
		return
	}
	ms, ok := f.msgs[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"id": id, "internalDate": fmt.Sprint(ms)})
}

func (f *fakeGmail) add(id string, ms int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs[id] = ms
}

func listGmail(t *testing.T, ctx context.Context, cfg map[string]interface{}) (string, error) {
	t.Helper()
	out, err := (&GmailNode{}).Execute(ctx, stateInput, cfg)
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(out[0].Items))
	for _, it := range out[0].Items {
		ids = append(ids, it.JSON["id"].(string))
	}
	return strings.Join(ids, ","), nil
}

func newFakeGmail(t *testing.T) *fakeGmail {
	f := &fakeGmail{msgs: map[string]int64{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	prev := gmailBaseURL
	gmailBaseURL = srv.URL
	t.Cleanup(func() { gmailBaseURL = prev })
	return f
}

func TestGmailListSinceLastRun(t *testing.T) {
	f := newFakeGmail(t)
	ctx := newStateContext(t)
	cfg := map[string]interface{}{"access_token": "tok", "operation": "list_messages", "query": "is:inbox", "since_last_run": true}

	f.add("a", 1_000_000_000_000)
	f.add("b", 1_000_000_005_500)
	got, err := listGmail(t, ctx, cfg)
	if err != nil || got != "b,a" {
		t.Fatalf("first run = %q, %v; want every message", got, err)
	}
	if f.queries[0] != "is:inbox" {
		t.Fatalf("first run q = %q, want the plain query", f.queries[0])
	}

	if got, err := listGmail(t, ctx, cfg); err != nil || got != "" {
		t.Fatalf("unchanged mailbox = %q, %v; want nothing", got, err)
	}
	// after: is in seconds and starts one second early so the boundary is
	// listed; the cursor's ids drop what was already emitted.
	if q := f.queries[1]; q != "is:inbox after:1000000004" {
		t.Fatalf("second run q = %q", q)
	}

	// c shares b's millisecond but is a different message; d is newer;
	// old is backdated below what was already emitted.
	f.add("c", 1_000_000_005_500)
	f.add("d", 1_000_000_009_000)
	f.add("old", 1_000_000_005_100)
	got, err = listGmail(t, ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got != "d,c" && got != "c,d" {
		t.Fatalf("third run = %q, want c and d", got)
	}
	if got, _ := listGmail(t, ctx, cfg); got != "" {
		t.Fatalf("fourth run = %q, want nothing", got)
	}

	// Without the flag every run lists the whole mailbox and stores nothing.
	plain := map[string]interface{}{"access_token": "tok", "operation": "list_messages", "query": "is:inbox"}
	if got, _ := listGmail(t, ctx, plain); got != "d,c,b,old,a" && got != "d,b,c,old,a" {
		t.Fatalf("since_last_run off = %q, want everything", got)
	}
}

func TestGmailListSinceLastRunKeepsCursorOnFailure(t *testing.T) {
	f := newFakeGmail(t)
	ctx := newStateContext(t)
	cfg := map[string]interface{}{"access_token": "tok", "operation": "list_messages", "since_last_run": true}

	f.add("a", 1_000_000_000_000)
	if got, err := listGmail(t, ctx, cfg); err != nil || got != "a" {
		t.Fatalf("first run = %q, %v", got, err)
	}
	f.add("b", 1_000_000_009_000)
	f.mu.Lock()
	f.failMeta = true
	f.mu.Unlock()
	if _, err := listGmail(t, ctx, cfg); err == nil || !strings.Contains(err.Error(), "since_last_run") {
		t.Fatalf("failed lookup err = %v, want a since_last_run error", err)
	}
	f.mu.Lock()
	f.failMeta = false
	f.mu.Unlock()
	if got, err := listGmail(t, ctx, cfg); err != nil || got != "b" {
		t.Fatalf("run after the failure = %q, %v; want b (cursor not advanced)", got, err)
	}
}

func TestGmailListSinceLastRunPerQuery(t *testing.T) {
	f := newFakeGmail(t)
	ctx := newStateContext(t)
	f.add("a", 1_000_000_000_000)
	cfg := map[string]interface{}{"access_token": "tok", "operation": "list_messages", "query": "x", "since_last_run": true}
	listGmail(t, ctx, cfg)
	cfg["query"] = "y"
	if got, _ := listGmail(t, ctx, cfg); got != "a" {
		t.Fatalf("new query = %q, want a first run", got)
	}
}

func TestGmailListSinceLastRunNeedsWorkflowState(t *testing.T) {
	newFakeGmail(t)
	_, err := (&GmailNode{}).Execute(context.Background(), stateInput,
		map[string]interface{}{"access_token": "tok", "operation": "list_messages", "since_last_run": true})
	if !errors.Is(err, workflow.ErrNoWorkflowState) {
		t.Fatalf("err = %v, want ErrNoWorkflowState", err)
	}
}

// fakeSheet serves one range's values.
type fakeSheet struct {
	mu     sync.Mutex
	rows   [][]string
	paths  []string
	failed bool
}

func (f *fakeSheet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, r.URL.EscapedPath())
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if f.failed {
		http.Error(w, "backend error", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"values": f.rows})
}

func (f *fakeSheet) set(rows ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = nil
	for _, r := range rows {
		f.rows = append(f.rows, strings.Split(r, "|"))
	}
}

func readSheet(t *testing.T, ctx context.Context, cfg map[string]interface{}) (string, error) {
	t.Helper()
	out, err := (&GoogleSheetsNode{}).Execute(ctx, stateInput, cfg)
	if err != nil {
		return "", err
	}
	var names []string
	for _, it := range out[0].Items {
		names = append(names, fmt.Sprintf("%v@%v", it.JSON["name"], it.JSON["_row_index"]))
	}
	return strings.Join(names, ","), nil
}

func newFakeSheet(t *testing.T) *fakeSheet {
	f := &fakeSheet{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	prev := sheetsBaseURL
	sheetsBaseURL = srv.URL
	t.Cleanup(func() { sheetsBaseURL = prev })
	return f
}

func TestSheetsReadRowsSinceLastRun(t *testing.T) {
	f := newFakeSheet(t)
	ctx := newStateContext(t)
	cfg := map[string]interface{}{"access_token": "tok", "operation": "read_rows", "spreadsheet_id": "S1",
		"sheet_name": "Leads", "use_header_row": true, "since_last_run": true}

	f.set("name|mail", "ann|a@x", "bob|b@x")
	if got, err := readSheet(t, ctx, cfg); err != nil || got != "ann@2,bob@3" {
		t.Fatalf("first run = %q, %v; want every row", got, err)
	}
	if f.paths[0] != "/S1/values/Leads" {
		t.Fatalf("path = %q", f.paths[0])
	}
	if got, _ := readSheet(t, ctx, cfg); got != "" {
		t.Fatalf("unchanged sheet = %q, want nothing", got)
	}
	f.set("name|mail", "ann|a@x", "bob|b@x", "cat|c@x", "dan|d@x")
	if got, _ := readSheet(t, ctx, cfg); got != "cat@4,dan@5" {
		t.Fatalf("appended rows = %q, want cat and dan", got)
	}

	// A failed read leaves the cursor where it was.
	f.set("name|mail", "ann|a@x", "bob|b@x", "cat|c@x", "dan|d@x", "eve|e@x")
	f.mu.Lock()
	f.failed = true
	f.mu.Unlock()
	if _, err := readSheet(t, ctx, cfg); err == nil {
		t.Fatal("failed read returned no error")
	}
	f.mu.Lock()
	f.failed = false
	f.mu.Unlock()
	if got, _ := readSheet(t, ctx, cfg); got != "eve@6" {
		t.Fatalf("run after the failure = %q, want eve", got)
	}

	// The sheet shrank (cleared and refilled): start over with every row.
	f.set("name|mail", "zed|z@x")
	if got, _ := readSheet(t, ctx, cfg); got != "zed@2" {
		t.Fatalf("shrunk sheet = %q, want every row", got)
	}
	f.set("name|mail", "zed|z@x", "yan|y@x")
	if got, _ := readSheet(t, ctx, cfg); got != "yan@3" {
		t.Fatalf("after the reset = %q, want yan", got)
	}

	// Another sheet of the same spreadsheet has its own cursor.
	cfg["sheet_name"] = "Other"
	if got, _ := readSheet(t, ctx, cfg); got != "zed@2,yan@3" {
		t.Fatalf("other sheet = %q, want a first run", got)
	}

	// Without the flag every row comes back.
	delete(cfg, "since_last_run")
	cfg["sheet_name"] = "Leads"
	if got, _ := readSheet(t, ctx, cfg); got != "zed@2,yan@3" {
		t.Fatalf("since_last_run off = %q", got)
	}
}

func TestSheetsReadRowsSinceLastRunNeedsWorkflowState(t *testing.T) {
	f := newFakeSheet(t)
	f.set("a")
	_, err := (&GoogleSheetsNode{}).Execute(context.Background(), stateInput,
		map[string]interface{}{"access_token": "tok", "operation": "read_rows", "spreadsheet_id": "S1", "since_last_run": true})
	if !errors.Is(err, workflow.ErrNoWorkflowState) {
		t.Fatalf("err = %v, want ErrNoWorkflowState", err)
	}
}

func TestGmailListSinceLastRunBaseline(t *testing.T) {
	f := newFakeGmail(t)
	ctx := newStateContext(t)
	cfg := map[string]interface{}{"access_token": "tok", "operation": "list_messages", "since_last_run": true, "baseline": true}

	f.add("a", 1_000_000_000_000)
	f.add("b", 1_000_000_005_500)
	if got, err := listGmail(t, ctx, cfg); err != nil || got != "" {
		t.Fatalf("baseline first run = %q, %v; want nothing", got, err)
	}
	f.add("c", 1_000_000_009_000)
	if got, err := listGmail(t, ctx, cfg); err != nil || got != "c" {
		t.Fatalf("second run = %q, %v; want c", got, err)
	}
}

func TestSheetsReadRowsSinceLastRunBaseline(t *testing.T) {
	f := newFakeSheet(t)
	ctx := newStateContext(t)
	cfg := map[string]interface{}{"access_token": "tok", "operation": "read_rows", "spreadsheet_id": "S1",
		"sheet_name": "Leads", "use_header_row": true, "since_last_run": true, "baseline": true}

	f.set("name|mail", "ann|a@x", "bob|b@x")
	if got, err := readSheet(t, ctx, cfg); err != nil || got != "" {
		t.Fatalf("baseline first run = %q, %v; want nothing", got, err)
	}
	f.set("name|mail", "ann|a@x", "bob|b@x", "cat|c@x")
	if got, _ := readSheet(t, ctx, cfg); got != "cat@4" {
		t.Fatalf("second run = %q, want cat", got)
	}
}
