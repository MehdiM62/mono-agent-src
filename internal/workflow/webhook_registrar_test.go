package workflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// fakeVendor stands in for a vendor API: it records every call and keeps
// the hooks that exist, keyed by URL path.
type fakeVendor struct {
	mu     sync.Mutex
	srv    *httptest.Server
	calls  []string // "METHOD /path"
	auth   []string
	bodies []map[string]interface{}
	hooks  map[string]bool
	// create answers a create call (path → response).
	create func(path string) interface{}
}

func newFakeVendor(t *testing.T, create func(path string) interface{}) *fakeVendor {
	v := &fakeVendor{hooks: map[string]bool{}, create: create}
	v.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		v.calls = append(v.calls, r.Method+" "+r.URL.Path)
		v.auth = append(v.auth, r.Header.Get("Authorization"))
		var body map[string]interface{}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		v.bodies = append(v.bodies, body)
		switch r.Method {
		case http.MethodPut, http.MethodPost:
			resp := v.create(r.URL.Path)
			if loc, ok := jsonPath(resp, "resource.uri").(string); ok {
				v.hooks[strings.TrimPrefix(loc, v.srv.URL)] = true
			} else {
				v.hooks[r.URL.Path] = true
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case http.MethodGet, http.MethodDelete:
			if !v.hooks[r.URL.Path] {
				http.NotFound(w, r)
				return
			}
			if r.Method == http.MethodDelete {
				delete(v.hooks, r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			}
		}
	}))
	t.Cleanup(v.srv.Close)
	return v
}

func (v *fakeVendor) callList() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.calls...)
}

func (v *fakeVendor) count(prefix string) int {
	n := 0
	for _, c := range v.callList() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// appTM is a trigger manager with a real (unstarted) webhook server, a
// credential resolver and app webhook records in db.
func appTM(t *testing.T, db *appWebhookStore) (*TriggerManager, *int) {
	t.Helper()
	fired := 0
	var mu sync.Mutex
	srv := NewWebhookServer(":0", zerolog.Nop())
	tm := NewTriggerManager(nil, srv, &fakeScheduler{}, func(string, string, []Item) {
		mu.Lock()
		fired++
		mu.Unlock()
	}, zerolog.Nop())
	tm.SetAppWebhooks(db.db, func(_ context.Context, profile, id string) (map[string]interface{}, error) {
		return map[string]interface{}{"access_token": "tok-" + id + "-" + profile}, nil
	})
	if db.db == nil {
		tm.apps.store = db // share the memory store between managers
	}
	return tm, &fired
}

func appPost(tm *TriggerManager, path string, body string, h map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhook/"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	tm.webhookServer.ServeHTTP(rec, req)
	return rec
}

func typeformWorkflow(tag string) *Workflow {
	cfg := map[string]interface{}{
		"path": "tf", "register_with": "typeform-test", "public_url": "https://pub.example/",
		"credential_id": "c1", "register_config": map[string]interface{}{"form_id": "F1"},
	}
	if tag != "" {
		cfg["register_config"].(map[string]interface{})["tag"] = tag
	}
	return &Workflow{ProfileID: "default", ID: "wf-tf", Nodes: []WorkflowNode{{ID: "n1", Type: TriggerNodeTypeWebhook, Config: cfg}}}
}

func typeformSig(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestAppWebhookTypeformLifecycle(t *testing.T) {
	vendor := newFakeVendor(t, func(p string) interface{} { return map[string]interface{}{"id": "hook-1", "tag": "monoagent-tf"} })
	RegisterWebhookRegistrar("typeform-test", TypeformRegistrar(vendor.srv.URL))
	store := &appWebhookStore{db: newStateDB(t)}
	tm, fired := appTM(t, store)
	ctx := context.Background()

	if err := tm.ActivateWorkflow(ctx, typeformWorkflow("")); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if got := vendor.callList(); len(got) != 1 || got[0] != "PUT /forms/F1/webhooks/monoagent-tf" {
		t.Fatalf("calls = %v", got)
	}
	if vendor.auth[0] != "Bearer tok-c1-default" {
		t.Fatalf("auth = %q", vendor.auth[0])
	}
	body := vendor.bodies[0]
	if body["url"] != "https://pub.example/webhook/tf" || body["enabled"] != true || body["secret"] == "" {
		t.Fatalf("create body = %v", body)
	}
	rec, err := store.get(ctx, "wf-tf", "n1")
	if err != nil || rec == nil || rec.HookID != "hook-1" || rec.Secret != body["secret"] {
		t.Fatalf("stored = %+v, %v", rec, err)
	}
	if _, ok := rec.Config["credential"]; ok {
		t.Fatal("the stored config holds the credential")
	}

	// Deliveries are checked against Typeform-Signature.
	delivery := `{"form_response":{}}`
	if r := appPost(tm, "tf", delivery, map[string]string{"Typeform-Signature": "sha256=AAAA"}); r.Code != 401 {
		t.Fatalf("bad signature: %d", r.Code)
	}
	if r := appPost(tm, "tf", delivery, map[string]string{"Typeform-Signature": typeformSig(rec.Secret, delivery)}); r.Code != 200 || *fired != 1 {
		t.Fatalf("good signature: %d fired %d", r.Code, *fired)
	}

	// Shutdown keeps the hook: the workflow is still active.
	tm.DeactivateAll()
	if n := vendor.count("DELETE"); n != 0 {
		t.Fatalf("DeactivateAll deleted %d hooks", n)
	}

	// Another process activates the same node: the stored hook is checked
	// and reused, not created again.
	tm2, _ := appTM(t, store)
	if err := tm2.ActivateWorkflow(ctx, typeformWorkflow("")); err != nil {
		t.Fatal(err)
	}
	if vendor.count("PUT") != 1 || vendor.count("GET /forms/F1/webhooks/monoagent-tf") != 1 {
		t.Fatalf("re-activation calls = %v", vendor.callList())
	}

	// Deactivation deletes the hook and forgets it.
	tm2.DeactivateWorkflow("wf-tf")
	if vendor.count("DELETE /forms/F1/webhooks/monoagent-tf") != 1 {
		t.Fatalf("deactivate calls = %v", vendor.callList())
	}
	if rec, _ := store.get(ctx, "wf-tf", "n1"); rec != nil {
		t.Fatalf("record kept after deactivation: %+v", rec)
	}
	if r := appPost(tm2, "tf", delivery, nil); r.Code != 404 {
		t.Fatalf("route after deactivation: %d", r.Code)
	}
}

func TestAppWebhookRegistersAgainWhenGoneOrChanged(t *testing.T) {
	vendor := newFakeVendor(t, func(p string) interface{} { return map[string]interface{}{"id": "h-" + p} })
	RegisterWebhookRegistrar("typeform-test", TypeformRegistrar(vendor.srv.URL))
	store := &appWebhookStore{}
	ctx := context.Background()

	tm, _ := appTM(t, store)
	if err := tm.ActivateWorkflow(ctx, typeformWorkflow("")); err != nil {
		t.Fatal(err)
	}
	tm.DeactivateAll()

	// Removed in the app by hand: the check finds it gone, so it is created again.
	vendor.mu.Lock()
	vendor.hooks = map[string]bool{}
	vendor.mu.Unlock()
	tm2, _ := appTM(t, store)
	if err := tm2.ActivateWorkflow(ctx, typeformWorkflow("")); err != nil {
		t.Fatal(err)
	}
	if vendor.count("PUT") != 2 {
		t.Fatalf("calls = %v", vendor.callList())
	}
	tm2.DeactivateAll()

	// Edited while no daemon ran: the old hook is removed, the new one made.
	tm3, _ := appTM(t, store)
	if err := tm3.ActivateWorkflow(ctx, typeformWorkflow("custom")); err != nil {
		t.Fatal(err)
	}
	if vendor.count("DELETE /forms/F1/webhooks/monoagent-tf") != 1 || vendor.count("PUT /forms/F1/webhooks/custom") != 1 {
		t.Fatalf("calls = %v", vendor.callList())
	}
}

func TestAppWebhookCalendlyNodeType(t *testing.T) {
	var vendor *fakeVendor
	vendor = newFakeVendor(t, func(p string) interface{} {
		return map[string]interface{}{"resource": map[string]interface{}{"uri": vendor.srv.URL + "/webhook_subscriptions/W1"}}
	})
	RegisterWebhookRegistrar("trigger.calendly_test", CalendlyRegistrar(vendor.srv.URL))
	t.Setenv(publicURLEnv, "https://tunnel.example")
	tm, fired := appTM(t, &appWebhookStore{})
	wf := &Workflow{ProfileID: "default", ID: "wf-cal", Nodes: []WorkflowNode{{ID: "c", Type: "trigger.calendly_test", Config: map[string]interface{}{
		"credential_id": "cal", "organization": "https://api.calendly.com/organizations/O1",
		"events": []interface{}{"invitee.created", "invitee.canceled"},
	}}}}
	if err := tm.ActivateWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("activate: %v", err)
	}
	path := appHookPath("wf-cal", "c")
	body := vendor.bodies[0]
	if vendor.callList()[0] != "POST /webhook_subscriptions" || body["url"] != "https://tunnel.example/webhook/"+path ||
		body["scope"] != "organization" || body["signing_key"] == "" {
		t.Fatalf("create: %v %v", vendor.callList(), body)
	}
	if ev, ok := body["events"].([]interface{}); !ok || len(ev) != 2 {
		t.Fatalf("events = %#v, want the list", body["events"])
	}
	if _, ok := body["user"]; ok {
		t.Fatal("an unset optional key was sent")
	}

	delivery := `{"event":"invitee.created"}`
	mac := hmac.New(sha256.New, []byte(body["signing_key"].(string)))
	mac.Write([]byte("1700000000." + delivery))
	sig := "t=1700000000,v1=" + hex.EncodeToString(mac.Sum(nil))
	if r := appPost(tm, path, delivery, map[string]string{"Calendly-Webhook-Signature": "t=1,v1=00"}); r.Code != 401 {
		t.Fatalf("bad signature: %d", r.Code)
	}
	if r := appPost(tm, path, delivery, map[string]string{"Calendly-Webhook-Signature": sig}); r.Code != 200 || *fired != 1 {
		t.Fatalf("good signature: %d fired %d", r.Code, *fired)
	}

	tm.DeactivateWorkflow("wf-cal")
	if vendor.count("DELETE /webhook_subscriptions/W1") != 1 {
		t.Fatalf("calls = %v", vendor.callList())
	}
}

func TestAppWebhookNeedsPublicURL(t *testing.T) {
	vendor := newFakeVendor(t, func(string) interface{} { return map[string]interface{}{"id": "x"} })
	RegisterWebhookRegistrar("typeform-test", TypeformRegistrar(vendor.srv.URL))
	t.Setenv(publicURLEnv, "")
	tm, _ := appTM(t, &appWebhookStore{})
	wf := typeformWorkflow("")
	delete(wf.Nodes[0].Config, "public_url")
	err := tm.ActivateWorkflow(context.Background(), wf)
	if err == nil || !strings.Contains(err.Error(), publicURLEnv) {
		t.Fatalf("err = %v, want one naming %s", err, publicURLEnv)
	}
	if r := appPost(tm, "tf", "{}", nil); r.Code != 404 {
		t.Fatalf("route left after a failed registration: %d", r.Code)
	}
	if len(vendor.callList()) != 0 {
		t.Fatalf("vendor called: %v", vendor.callList())
	}
}

func TestAppWebhookUnknownRegistrar(t *testing.T) {
	tm, _ := appTM(t, &appWebhookStore{})
	wf := typeformWorkflow("")
	wf.Nodes[0].Config["register_with"] = "no-such-app"
	if err := tm.ActivateWorkflow(context.Background(), wf); err == nil || !strings.Contains(err.Error(), "no-such-app") {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPRegistrarHookURLStaysOnBaseHost(t *testing.T) {
	h := &HTTPRegistrar{BaseURL: "https://api.calendly.com"}
	vars := templateVars{stored: AppWebhookHook{ID: "https://evil.example/x"}}
	if _, err := h.callURL(vars, "{{hook_id}}"); err == nil {
		t.Fatal("a hook URL on another host was accepted")
	}
	vars.stored.ID = "https://api.calendly.com/webhook_subscriptions/1"
	if got, err := h.callURL(vars, "{{hook_id}}"); err != nil || got != vars.stored.ID {
		t.Fatalf("got %q, %v", got, err)
	}
	vars.hook.Config = map[string]interface{}{"form_id": "a/b"}
	if got, _ := h.callURL(vars, "/forms/{{config.form_id}}"); got != "https://api.calendly.com/forms/a%2Fb" {
		t.Fatalf("escaped path = %q", got)
	}
}

// TestReconcileUnregistersAppWebhook: a deactivation made by another process
// reaches the daemon through the reconcile loop, which removes the hook.
func TestReconcileUnregistersAppWebhook(t *testing.T) {
	vendor := newFakeVendor(t, func(string) interface{} { return map[string]interface{}{"id": "h1"} })
	RegisterWebhookRegistrar("typeform-test", TypeformRegistrar(vendor.srv.URL))
	store := newFullEngineStore(t)
	ctx := context.Background()
	eng, _ := newDaemonEngine(t, store)
	eng.triggerMgr.SetAppWebhooks(nil, func(context.Context, string, string) (map[string]interface{}, error) {
		return map[string]interface{}{"token": "t"}, nil
	})
	node := typeformWorkflow("").Nodes[0]
	saveTriggerWorkflow(t, ctx, store, "wf-tf", node)
	if err := store.SetWorkflowActive(ctx, "wf-tf", true); err != nil {
		t.Fatal(err)
	}
	eng.reconcileTriggers(ctx)
	if vendor.count("PUT") != 1 || webhookRoute(eng, "tf") == nil {
		t.Fatalf("after activation elsewhere: %v", vendor.callList())
	}
	if err := store.SetWorkflowActive(ctx, "wf-tf", false); err != nil {
		t.Fatal(err)
	}
	eng.reconcileTriggers(ctx)
	if vendor.count("DELETE") != 1 || webhookRoute(eng, "tf") != nil {
		t.Fatalf("after deactivation elsewhere: %v", vendor.callList())
	}
}
