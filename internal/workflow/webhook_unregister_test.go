package workflow

import (
	"context"
	"testing"
)

// cliEngineOver is a one-shot engine (the CLI's) over db: no daemon, no
// trigger entries of its own.
func cliEngineOver(t *testing.T, store *SQLiteWorkflowStore) *WorkflowEngine {
	t.Helper()
	eng := newTestEngine(t, store, NewNodeTypeRegistry())
	eng.triggerMgr.SetAppWebhooks(store.RawDB(), func(_ context.Context, profile, id string) (map[string]interface{}, error) {
		return map[string]interface{}{"access_token": "tok-" + id + "-" + profile}, nil
	})
	return eng
}

// daemonRegistered stores an active typeform workflow and registers its hook
// the way a daemon does, then drops the daemon (shutdown keeps the hook).
func daemonRegistered(t *testing.T, ctx context.Context, store *SQLiteWorkflowStore, vendor *fakeVendor) {
	t.Helper()
	wf := typeformWorkflow("")
	saveTriggerWorkflow(t, ctx, store, wf.ID, wf.Nodes[0])
	if err := store.SetWorkflowActive(ctx, wf.ID, true); err != nil {
		t.Fatal(err)
	}
	daemon, _ := appTM(t, &appWebhookStore{db: store.RawDB()})
	if err := daemon.ActivateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	daemon.DeactivateAll()
	if vendor.count("PUT") != 1 || vendor.count("DELETE") != 0 {
		t.Fatalf("daemon calls = %v", vendor.callList())
	}
}

func TestCLIDeactivateUnregistersStoredAppWebhook(t *testing.T) {
	vendor := newFakeVendor(t, func(string) interface{} { return map[string]interface{}{"id": "hook-1"} })
	RegisterWebhookRegistrar("typeform-test", TypeformRegistrar(vendor.srv.URL))
	store := NewSQLiteWorkflowStore(newStateDB(t))
	ctx := context.Background()
	daemonRegistered(t, ctx, store, vendor)

	eng := cliEngineOver(t, store)
	if err := eng.DeactivateWorkflow(ctx, "wf-tf"); err != nil {
		t.Fatal(err)
	}
	if vendor.count("DELETE /forms/F1/webhooks/monoagent-tf") != 1 {
		t.Fatalf("deactivate calls = %v", vendor.callList())
	}
	if vendor.auth[len(vendor.auth)-1] != "Bearer tok-c1-default" {
		t.Fatalf("unregister auth = %q", vendor.auth[len(vendor.auth)-1])
	}
	if rec, _ := (&appWebhookStore{db: store.RawDB()}).get(ctx, "wf-tf", "n1"); rec != nil {
		t.Fatalf("record kept: %+v", rec)
	}
	// Nothing left to remove a second time.
	if err := eng.DeactivateWorkflow(ctx, "wf-tf"); err != nil {
		t.Fatal(err)
	}
	if n := vendor.count("DELETE"); n != 1 {
		t.Fatalf("second deactivate: %d deletes", n)
	}
}

func TestCLIDeleteUnregistersStoredAppWebhook(t *testing.T) {
	vendor := newFakeVendor(t, func(string) interface{} { return map[string]interface{}{"id": "hook-1"} })
	RegisterWebhookRegistrar("typeform-test", TypeformRegistrar(vendor.srv.URL))
	store := NewSQLiteWorkflowStore(newStateDB(t))
	ctx := context.Background()
	daemonRegistered(t, ctx, store, vendor)

	eng := cliEngineOver(t, store)
	if err := eng.DeleteWorkflow(ctx, "wf-tf"); err != nil {
		t.Fatal(err)
	}
	if vendor.count("DELETE /forms/F1/webhooks/monoagent-tf") != 1 {
		t.Fatalf("delete calls = %v", vendor.callList())
	}
	if rec, _ := (&appWebhookStore{db: store.RawDB()}).get(ctx, "wf-tf", "n1"); rec != nil {
		t.Fatalf("record kept: %+v", rec)
	}
}

// TestStoredAppHooksScopedToWorkflow: only the workflow's own rows go.
func TestStoredAppHooksScopedToWorkflow(t *testing.T) {
	vendor := newFakeVendor(t, func(string) interface{} { return map[string]interface{}{"id": "hook-1"} })
	RegisterWebhookRegistrar("typeform-test", TypeformRegistrar(vendor.srv.URL))
	db := newStateDB(t)
	ctx := context.Background()
	s := &appWebhookStore{db: db}
	for _, wf := range []string{"wf-a", "wf-ab"} {
		if err := s.put(ctx, appWebhookRecord{WorkflowID: wf, NodeID: "n1", Registrar: "typeform-test", HookID: "h",
			URL: "https://pub.example/webhook/x", Config: map[string]interface{}{"form_id": "F1"}}); err != nil {
			t.Fatal(err)
		}
	}
	tm, _ := appTM(t, s)
	tm.unregisterStoredAppHooks(ctx, "wf-a", "default")
	if a, _ := s.get(ctx, "wf-a", "n1"); a != nil {
		t.Fatal("wf-a row kept")
	}
	if b, _ := s.get(ctx, "wf-ab", "n1"); b == nil {
		t.Fatal("wf-ab row removed")
	}
}
