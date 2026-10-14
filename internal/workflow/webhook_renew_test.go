package workflow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// graphVendor stands in for Microsoft Graph's /subscriptions: create and
// PATCH answer the expirationDateTime asked for; fail makes PATCH answer it.
type graphVendor struct {
	mu     sync.Mutex
	srv    *httptest.Server
	calls  []string
	bodies []map[string]interface{}
	fail   int // status PATCH answers instead; 0 = success
}

func newGraphVendor(t *testing.T) *graphVendor {
	v := &graphVendor{}
	v.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.calls = append(v.calls, r.Method+" "+r.URL.Path)
		var body map[string]interface{}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		v.bodies = append(v.bodies, body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/subscriptions":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "sub-1", "expirationDateTime": body["expirationDateTime"]})
		case r.Method == http.MethodPatch && r.URL.Path == "/v1.0/subscriptions/sub-1":
			if v.fail != 0 {
				w.WriteHeader(v.fail)
				_, _ = w.Write([]byte(`{"error":{"code":"ServiceUnavailable"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "sub-1", "expirationDateTime": body["expirationDateTime"]})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.srv.Close)
	return v
}

func (v *graphVendor) count(prefix string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	for _, c := range v.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// graphRegistrar is how a Teams/Outlook trigger declares a Graph
// subscription that lives lifetime.
func graphRegistrar(base string, lifetime time.Duration) *HTTPRegistrar {
	return &HTTPRegistrar{
		BaseURL: base,
		Auth:    "Bearer {{credential.access_token}}",
		Create: HTTPCall{Method: http.MethodPost, URL: "/v1.0/subscriptions", Body: map[string]interface{}{
			"changeType": "created", "notificationUrl": "{{url}}", "resource": "{{config.resource}}",
			"expirationDateTime": "{{expires_at}}", "clientState": "{{secret}}",
		}},
		IDPath:      "id",
		Lifetime:    lifetime,
		ExpiresPath: "expirationDateTime",
		RenewCall: &HTTPCall{Method: http.MethodPatch, URL: "/v1.0/subscriptions/{{hook_id}}",
			Body: map[string]interface{}{"expirationDateTime": "{{expires_at}}"}},
		Delete: HTTPCall{Method: http.MethodDelete, URL: "/v1.0/subscriptions/{{hook_id}}"},
	}
}

func graphWorkflow() *Workflow {
	return &Workflow{ID: "wf-graph", Nodes: []WorkflowNode{{ID: "n1", Type: TriggerNodeTypeWebhook, Config: map[string]interface{}{
		"path": "teams", "register_with": "graph-test", "public_url": "https://pub.example",
		"credential_id": "ms", "register_config": map[string]interface{}{"resource": "/chats/getAllMessages"},
	}}}}
}

func TestHTTPRegistrarGraphRenew(t *testing.T) {
	v := newGraphVendor(t)
	r := graphRegistrar(v.srv.URL, 70*time.Hour)
	hook := AppWebhook{URL: "https://pub.example/webhook/teams", Secret: "s",
		Config: map[string]interface{}{"resource": "/me/messages", "credential": map[string]interface{}{"access_token": "t"}}}
	before := time.Now()
	got, err := r.Register(context.Background(), hook)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "sub-1" || got.ExpiresAt.Before(before.Add(70*time.Hour-time.Second)) || got.ExpiresAt.After(time.Now().Add(70*time.Hour)) {
		t.Fatalf("register = %+v", got)
	}
	exp, err := r.Renew(context.Background(), hook, got)
	if err != nil {
		t.Fatal(err)
	}
	if v.count("PATCH /v1.0/subscriptions/sub-1") != 1 || v.bodies[1]["expirationDateTime"] != exp.Format(time.RFC3339) {
		t.Fatalf("renew call %v body %v, expiry %v", v.calls, v.bodies[1], exp)
	}
}

// Jira Cloud's dynamic webhooks: the id is a number inside a list, the
// refresh takes the ids and answers expirationDate in Jira's format.
func TestHTTPRegistrarJiraRefresh(t *testing.T) {
	var refreshBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /rest/api/3/webhook":
			_, _ = w.Write([]byte(`{"webhookRegistrationResult":[{"createdWebhookId":1000}]}`))
		case "PUT /rest/api/3/webhook/refresh":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &refreshBody)
			_, _ = w.Write([]byte(`{"expirationDate":"2026-11-13T09:00:00.000+0000"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := &HTTPRegistrar{
		BaseURL: srv.URL,
		Create: HTTPCall{Method: http.MethodPost, URL: "/rest/api/3/webhook", Body: map[string]interface{}{
			"url": "{{url}}", "webhooks": []interface{}{map[string]interface{}{"events": []interface{}{"jira:issue_created"}, "jqlFilter": "project = X"}},
		}},
		IDPath:     "webhookRegistrationResult.0.createdWebhookId",
		ExtraPaths: map[string]string{"webhook_id": "webhookRegistrationResult.0.createdWebhookId"},
		Lifetime:   30 * 24 * time.Hour,
		RenewCall: &HTTPCall{Method: http.MethodPut, URL: "/rest/api/3/webhook/refresh",
			Body: map[string]interface{}{"webhookIds": []interface{}{"{{extra.webhook_id}}"}}},
		ExpiresPath: "expirationDate",
		Delete:      HTTPCall{Method: http.MethodDelete, URL: "/rest/api/3/webhook"},
	}
	hook := AppWebhook{URL: "https://pub.example/webhook/jira", Config: map[string]interface{}{}}
	r.ExpiresPath = "" // the create answer has no expiry: now + Lifetime
	got, err := r.Register(context.Background(), hook)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "1000" || got.ExpiresAt.Before(time.Now().Add(29*24*time.Hour)) {
		t.Fatalf("register = %+v", got)
	}
	r.ExpiresPath = "expirationDate"
	exp, err := r.Renew(context.Background(), hook, got)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC); !exp.Equal(want) {
		t.Fatalf("expiry = %v, want %v", exp, want)
	}
	if ids, _ := refreshBody["webhookIds"].([]interface{}); len(ids) != 1 || ids[0] != float64(1000) {
		t.Fatalf("refresh body = %v (the id must stay a number)", refreshBody)
	}
}

func TestRenewDue(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	rec := &appWebhookRecord{HookID: "h", RenewedAt: t0, ExpiresAt: t0.Add(4 * time.Hour)}
	for _, c := range []struct {
		at   time.Duration
		want bool
	}{{0, false}, {2 * time.Hour, false}, {3*time.Hour - time.Second, false}, {3 * time.Hour, true}, {5 * time.Hour, true}} {
		if got := renewDue(rec, t0.Add(c.at)); got != c.want {
			t.Errorf("at +%v: due = %v, want %v", c.at, got, c.want)
		}
	}
	if renewDue(&appWebhookRecord{HookID: "h"}, t0) {
		t.Error("a hook without expiry is due")
	}
}

func TestAppWebhookRenewal(t *testing.T) {
	v := newGraphVendor(t)
	RegisterWebhookRegistrar("graph-test", graphRegistrar(v.srv.URL, 4*time.Hour))
	store := &appWebhookStore{db: newStateDB(t)}
	tm, _ := appTM(t, store)
	ctx := context.Background()
	if err := tm.ActivateWorkflow(ctx, graphWorkflow()); err != nil {
		t.Fatal(err)
	}
	rec, _ := store.get(ctx, "wf-graph", "n1")
	if rec == nil || rec.HookID != "sub-1" || rec.ExpiresAt.IsZero() || rec.RenewedAt.IsZero() {
		t.Fatalf("stored = %+v", rec)
	}
	start := rec.RenewedAt

	// Before 75% of the lifetime: nothing.
	tm.renewAppHooks(ctx, start.Add(2*time.Hour))
	if n := v.count("PATCH"); n != 0 {
		t.Fatalf("renewed early: %d", n)
	}

	// Due, and the vendor fails: the error and its class are kept, and the
	// retry waits for the backoff.
	v.mu.Lock()
	v.fail = http.StatusServiceUnavailable
	v.mu.Unlock()
	due := start.Add(3*time.Hour + time.Minute)
	tm.renewAppHooks(ctx, due)
	rec, _ = store.get(ctx, "wf-graph", "n1")
	if v.count("PATCH") != 1 || rec.RenewError == "" || rec.RenewErrorClass != string(ClassUpstream5xx) {
		t.Fatalf("after a failed renewal: patches %d, row %+v", v.count("PATCH"), rec)
	}
	tm.renewAppHooks(ctx, due.Add(appHookRenewBackoff/2))
	if n := v.count("PATCH"); n != 1 {
		t.Fatalf("retried before the backoff: %d", n)
	}
	tm.renewAppHooks(ctx, due.Add(appHookRenewBackoff))
	if n := v.count("PATCH"); n != 2 {
		t.Fatalf("not retried after the backoff: %d", n)
	}
	// Second failure doubles the wait.
	tm.renewAppHooks(ctx, due.Add(2*appHookRenewBackoff))
	if n := v.count("PATCH"); n != 2 {
		t.Fatalf("retried before the doubled backoff: %d", n)
	}

	// The vendor recovers: the new expiry (the registrar's clock: now + 4h)
	// is stored and the error cleared.
	v.mu.Lock()
	v.fail = 0
	v.mu.Unlock()
	at := due.Add(3 * appHookRenewBackoff)
	tm.renewAppHooks(ctx, at)
	rec, _ = store.get(ctx, "wf-graph", "n1")
	if v.count("PATCH") != 3 || rec.RenewError != "" || rec.RenewErrorClass != "" || !rec.RenewedAt.Equal(at) ||
		!rec.ExpiresAt.After(time.Now().Add(3*time.Hour)) {
		t.Fatalf("after the renewal: patches %d, row %+v", v.count("PATCH"), rec)
	}
	// Not due again until 75% of the new lifetime.
	tm.renewAppHooks(ctx, at.Add(time.Minute))
	if n := v.count("PATCH"); n != 3 {
		t.Fatalf("renewed again at once: %d", n)
	}

	// A deactivated workflow's hook is not renewed.
	tm.DeactivateWorkflow("wf-graph")
	tm.renewAppHooks(ctx, at.Add(48*time.Hour))
	if n := v.count("PATCH"); n != 3 {
		t.Fatalf("renewed after deactivation: %d", n)
	}
}

// An activation that finds the stored hook expired registers a new one
// instead of reusing it.
func TestAppWebhookExpiredNotReused(t *testing.T) {
	v := newGraphVendor(t)
	RegisterWebhookRegistrar("graph-test", graphRegistrar(v.srv.URL, 4*time.Hour))
	store := &appWebhookStore{db: newStateDB(t)}
	tm, _ := appTM(t, store)
	ctx := context.Background()
	if err := tm.ActivateWorkflow(ctx, graphWorkflow()); err != nil {
		t.Fatal(err)
	}
	tm.DeactivateAll()
	if _, err := store.db.Exec(`UPDATE app_webhooks SET expires_at = ?`, time.Now().Add(-time.Minute).UTC()); err != nil {
		t.Fatal(err)
	}
	tm2, _ := appTM(t, store)
	if err := tm2.ActivateWorkflow(ctx, graphWorkflow()); err != nil {
		t.Fatal(err)
	}
	if v.count("POST") != 2 || v.count("DELETE") != 1 {
		t.Fatalf("calls = %v", v.calls)
	}
	if rec, _ := store.get(ctx, "wf-graph", "n1"); rec == nil || !rec.ExpiresAt.After(time.Now()) {
		t.Fatalf("stored = %+v", rec)
	}
}
