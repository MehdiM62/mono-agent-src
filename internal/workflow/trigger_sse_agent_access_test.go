package workflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/secrets"
)

// With the owner's agent access on, an unapproved stream sends its @secret
// header only to a host the grant names, audited by name; an approval check
// that failed does not lift the refusal.
func TestAnUnapprovedSSETriggerUnderAgentAccess(t *testing.T) {
	keyring.MockInit()
	db, store := approvalsTestDB(t)
	ctx := context.Background()
	if _, err := secrets.Add(ctx, db.DB, "default", "secret", "feed", map[string]string{"secret": "vsecret-777"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	wf := approvalsTestWorkflow("wf-sse2", 1)
	wf.Nodes[0] = WorkflowNode{ID: "wf-sse2-t", WorkflowID: "wf-sse2", Type: TriggerNodeTypeSSE, Name: "Stream",
		Config: map[string]interface{}{"url": srv.URL, "headers": `{"Authorization": "@secret:feed"}`}}
	createSQLWorkflow(t, store, wf)
	src := NewSSETriggerSource(db.DB, nil)

	// Bound to another host: refused before any request.
	if err := secrets.SetAgentAccessGrant(ctx, db.DB, "default", secrets.AccessGrant{Hosts: []string{"feeds.example"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Activate(wf, &wf.Nodes[0], func([]Item) {}); !errors.Is(err, secrets.ErrHostNotAllowed) || strings.Contains(err.Error(), "vsecret-777") {
		t.Fatalf("a stream to a host the grant does not name: %v", err)
	}
	mu.Lock()
	n := len(auths)
	mu.Unlock()
	if n != 0 {
		t.Fatal("the request was made")
	}

	// Bound to its host: connects with the value, and the read is audited.
	if err := secrets.SetAgentAccessGrant(ctx, db.DB, "default", secrets.AccessGrant{Hosts: []string{"127.0.0.1"}}); err != nil {
		t.Fatal(err)
	}
	stop, err := src.Activate(wf, &wf.Nodes[0], func([]Item) {})
	if err != nil {
		t.Fatalf("a stream to the granted host: %v", err)
	}
	defer stop()
	for i := 0; i < 200; i++ {
		mu.Lock()
		n = len(auths)
		mu.Unlock()
		if n > 0 {
			break
		}
		<-time.After(10 * time.Millisecond)
	}
	mu.Lock()
	if len(auths) == 0 || auths[0] != "vsecret-777" {
		t.Fatalf("the stream sent %q", auths)
	}
	mu.Unlock()
	rows, _ := secrets.AgentAccessAudit(ctx, db.DB, "default")
	if len(rows) == 0 || rows[0].Secret != "feed" || rows[0].WorkflowID != "wf-sse2" || rows[0].NodeID != "wf-sse2-t" {
		t.Errorf("audit: %v", rows)
	}
}

func TestAnSSETriggerWhoseApprovalCheckFailsIsNotLiftedByAgentAccess(t *testing.T) {
	keyring.MockInit()
	db, store := approvalsTestDB(t)
	ctx := context.Background()
	if _, err := secrets.Add(ctx, db.DB, "default", "secret", "feed", map[string]string{"secret": "vsecret-777"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	wf := approvalsTestWorkflow("wf-sse3", 1)
	wf.Nodes[0] = WorkflowNode{ID: "wf-sse3-t", WorkflowID: "wf-sse3", Type: TriggerNodeTypeSSE, Name: "Stream",
		Config: map[string]interface{}{"url": srv.URL, "headers": `{"Authorization": "@secret:feed"}`}}
	createSQLWorkflow(t, store, wf)
	if err := secrets.SetAgentAccessGrant(ctx, db.DB, "default", secrets.AccessGrant{AnyHost: true}); err != nil {
		t.Fatal(err)
	}
	// The approvals table now answers with an error (not a missing table).
	for _, q := range []string{
		`ALTER TABLE workflow_approvals RENAME TO workflow_approvals_kept`,
		`CREATE VIEW workflow_approvals AS SELECT 'a' AS workflow_id, 1 AS definition_hash WHERE like('a', 'b', 'xx')`,
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	_, err := NewSSETriggerSource(db.DB, nil).Activate(wf, &wf.Nodes[0], func([]Item) {})
	if err == nil || !strings.Contains(err.Error(), "could not be checked") || strings.Contains(err.Error(), "vsecret-777") {
		t.Fatalf("a stream whose approval check failed: %v", err)
	}
}
