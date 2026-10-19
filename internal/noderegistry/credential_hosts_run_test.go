package noderegistry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/workflow"
)

// mastodonFake records the Authorization of every request it gets.
type mastodonFake struct {
	*httptest.Server
	mu    sync.Mutex
	auths []string
}

func newMastodonFake(t *testing.T) *mastodonFake {
	f := &mastodonFake{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","username":"me"}`))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *mastodonFake) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths...)
}

// The real service.mastodon in a run: the user's approved workflow with
// the connection's instance written in reaches it with the token; a run of
// a workflow the user never approved (an agent's) naming another instance,
// and the user's own naming one the connection doesn't, are refused, and
// that instance gets nothing.
func TestMastodonRunsSendTheTokenOnlyToTheConnectionsInstance(t *testing.T) {
	asOperator(t)
	db := sendsDB(t)
	ctx := sendsCtx(db)
	own, other := newMastodonFake(t), newMastodonFake(t)
	conn := &connections.Connection{ProfileID: "default", Platform: "mastodon", Method: connections.MethodAPIKey, Label: "fosstodon",
		Data: map[string]interface{}{"instance_url": own.URL, "access_token": "masto-tok"}}
	conns := connections.NewStore(db)
	if err := conns.Save(ctx, conn); err != nil {
		t.Fatal(err)
	}
	store := workflow.NewSQLiteWorkflowStore(db)
	reg := Build(db)
	run := func(approved bool, instance string) error {
		t.Helper()
		id := "wf-" + uuid.NewString()[:8]
		wf := &workflow.Workflow{ID: id, Name: id, ProfileID: "default", Nodes: []workflow.WorkflowNode{
			{ID: id + "-t", WorkflowID: id, Type: "trigger.manual", Name: "Start"},
			{ID: id + "-m", WorkflowID: id, Type: "service.mastodon", Name: "Mastodon", Config: map[string]interface{}{
				"credential_id": conn.ID, "operation": "get_account", "instance": instance}},
		}, Connections: []workflow.WorkflowConnection{{ID: id + "-c", WorkflowID: id, SourceNodeID: id + "-t", SourceHandle: "main", TargetNodeID: id + "-m", TargetHandle: "main"}}}
		if err := store.CreateWorkflow(ctx, wf); err != nil {
			t.Fatal(err)
		}
		if approved {
			if err := store.ApproveDefinition(ctx, id); err != nil {
				t.Fatal(err)
			}
		}
		dag, err := workflow.BuildDAG(wf.Nodes, wf.Connections)
		if err != nil {
			t.Fatal(err)
		}
		exec := &workflow.WorkflowExecution{ID: uuid.NewString(), WorkflowID: id, ProfileID: "default", TriggerType: "trigger.manual", TriggerNodeID: id + "-t", TriggerData: map[string]interface{}{}}
		return workflow.RunExecution(context.Background(), exec, wf, dag, reg, store, conns, workflow.NewExpressionEngine(), zerolog.Nop())
	}
	if err := run(true, own.URL); err != nil {
		t.Fatalf("the user's workflow with the connection's instance: %v", err)
	}
	if got := own.got(); len(got) != 1 || got[0] != "Bearer masto-tok" {
		t.Fatalf("the connection's instance got %q", got)
	}
	for _, approved := range []bool{false, true} {
		if err := run(approved, other.URL); !errors.Is(err, workflow.ErrCredentialDestination) {
			t.Errorf("approved %v, another instance: %v", approved, err)
		}
	}
	if got := other.got(); len(got) != 0 {
		t.Fatalf("the other instance got %q", got)
	}
}
