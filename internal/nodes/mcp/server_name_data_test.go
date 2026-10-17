package mcpnode

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// mcp.call reads the server definition the workflow names. A name an
// expression takes from a webhook's body is refused before the vault is
// read, in an approved workflow too; a name written in the workflow is read
// as before (#845 review: data-chosen vault entries).
func TestCallReadsOnlyTheServerTheWorkflowNames(t *testing.T) {
	keyring.MockInit()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	if _, err := secrets.Add(ctx, db.DB, "default", "secret", "crm", map[string]string{"secret": "not a server definition"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	store := workflow.NewSQLiteWorkflowStore(db.DB)
	run := func(server string) error {
		t.Helper()
		id := "wf-" + uuid.NewString()[:8]
		wf := &workflow.Workflow{ID: id, Name: id, ProfileID: "default",
			Nodes: []workflow.WorkflowNode{
				{ID: id + "-t", WorkflowID: id, Type: "trigger.webhook", Name: "Hook"},
				{ID: id + "-m", WorkflowID: id, Type: NodeType, Name: "Call", Config: map[string]interface{}{"server": server, "operation": "list_tools"}},
			},
			Connections: []workflow.WorkflowConnection{{ID: id + "-c", WorkflowID: id, SourceNodeID: id + "-t", SourceHandle: "main", TargetNodeID: id + "-m", TargetHandle: "main"}},
		}
		for _, err := range []error{store.CreateWorkflow(ctx, wf), store.SaveWorkflowNodes(ctx, id, wf.Nodes),
			store.SaveWorkflowConnections(ctx, id, wf.Connections), store.ApproveDefinition(ctx, id)} {
			if err != nil {
				t.Fatal(err)
			}
		}
		dag, err := workflow.BuildDAG(wf.Nodes, wf.Connections)
		if err != nil {
			t.Fatal(err)
		}
		reg := workflow.NewNodeTypeRegistry()
		reg.Register(NodeType, func() workflow.NodeExecutor { return &CallNode{} })
		exec := &workflow.WorkflowExecution{ID: uuid.NewString(), WorkflowID: id, ProfileID: "default", TriggerType: "trigger.webhook", TriggerNodeID: id + "-t",
			TriggerData: map[string]interface{}{"body": map[string]interface{}{"server": "crm"}}}
		return workflow.RunExecution(ctx, exec, wf, dag, reg, store, nil, workflow.NewExpressionEngine(), zerolog.Nop())
	}

	const refused = `"server": the vault entry's name must be written in the workflow, not taken from its data`
	if err := run("{{ $json.body.server }}"); err == nil || !strings.Contains(err.Error(), refused) {
		t.Errorf("a server named by the body: %v", err)
	}
	// The workflow's own name is read: this entry holds no server definition, which is what fails.
	if err := run("crm"); err == nil || strings.Contains(err.Error(), refused) || !strings.Contains(err.Error(), `server secret "crm" is not a JSON server definition`) {
		t.Errorf("the workflow's own server: %v", err)
	}
}
