package workflow_test

import (
	"context"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/workflow"
	"github.com/rs/zerolog"
	"github.com/zalando/go-keyring"
)

// cfgRec records the config of its last run.
type cfgRec struct {
	mu  *sync.Mutex
	cfg *map[string]interface{}
}

func (cfgRec) Type() string { return "test.cfg" }
func (c cfgRec) Execute(_ context.Context, in workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.cfg = config
	return []workflow.NodeOutput{{Handle: "main", Items: in.Items}}, nil
}

// Connection fields fill the keys a node leaves unset; they never replace
// a key the node's config sets.
func TestCredentialFillsOnlyUnsetKeys(t *testing.T) {
	keyring.MockInit()
	db := testdb.Open(t).DB
	store := connections.NewStore(db)
	c := &connections.Connection{Platform: "github", Method: connections.MethodAPIKey, Label: "gh",
		Data: map[string]interface{}{"url": "https://conn.example", "token": "t1", "region": "eu"}}
	if err := store.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	rec := cfgRec{mu: &sync.Mutex{}, cfg: &map[string]interface{}{}}
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("test.cfg", func() workflow.NodeExecutor { return rec })
	wf := &workflow.Workflow{ID: "wf", Nodes: []workflow.WorkflowNode{
		{ID: "t", Type: "trigger.manual", Name: "Start"},
		{ID: "n", Type: "test.cfg", Name: "N", Config: map[string]interface{}{
			"credential_id": c.ID, "url": "https://node.example", "region": "",
		}},
	}, Connections: []workflow.WorkflowConnection{conn("t", "main", "n")}}
	dag, err := workflow.BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	exec := &workflow.WorkflowExecution{ID: "e", WorkflowID: wf.ID, TriggerData: map[string]interface{}{}}
	if err := workflow.RunExecution(context.Background(), exec, wf, dag, reg, &recStore{}, store, workflow.NewExpressionEngine(), zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	got := *rec.cfg
	if got["url"] != "https://node.example" {
		t.Errorf("url = %v, want the node's own url", got["url"])
	}
	if got["token"] != "t1" || got["region"] != "eu" {
		t.Errorf("token %v region %v, want the connection's values for unset keys", got["token"], got["region"])
	}
	if cred, _ := got["credential"].(map[string]interface{}); cred["url"] != "https://conn.example" {
		t.Errorf("credential = %v, want the full connection data", got["credential"])
	}
}
