package vault

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/secrets"
	vaultstore "github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Agent access (`secret agent-access on`) lets an unapproved workflow use
// @secret references in the fields that take them. vault.secret_get puts the
// plaintext in the run's output, so it stays refused to that workflow, with
// access on as off; the workflow's output never holds the value.
func TestSecretGetStaysRefusedToAnUnapprovedWorkflowUnderAgentAccess(t *testing.T) {
	ctx := newSecretsTestCtx(t)
	db := vaultstore.DBFromContext(ctx)
	if _, err := secrets.Add(ctx, db, "default", "secret", "vs", map[string]string{"secret": "vsecret-777"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	store := workflow.NewSQLiteWorkflowStore(db)
	id := "wf-" + uuid.NewString()[:8]
	wf := &workflow.Workflow{ID: id, Name: id, ProfileID: "default",
		Nodes: []workflow.WorkflowNode{
			{ID: id + "-t", WorkflowID: id, Type: "trigger.schedule", Name: "Hourly"},
			{ID: id + "-g", WorkflowID: id, Type: "vault.secret_get", Name: "Get", Config: map[string]interface{}{"name": "vs", "output_field": "got"}},
		},
		Connections: []workflow.WorkflowConnection{{ID: id + "-c", WorkflowID: id, SourceNodeID: id + "-t", SourceHandle: "main", TargetNodeID: id + "-g", TargetHandle: "main"}},
	}
	if err := store.CreateWorkflow(ctx, wf); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWorkflowNodes(ctx, id, wf.Nodes); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWorkflowConnections(ctx, id, wf.Connections); err != nil {
		t.Fatal(err)
	}
	dag, err := workflow.BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	reg := workflow.NewNodeTypeRegistry()
	reg.Register("vault.secret_get", func() workflow.NodeExecutor { return &SecretGetNode{} })
	run := func() (string, error) {
		exec := &workflow.WorkflowExecution{ID: uuid.NewString(), WorkflowID: id, ProfileID: "default", TriggerType: "trigger.schedule", TriggerNodeID: id + "-t"}
		if err := store.CreateExecution(ctx, exec); err != nil {
			t.Fatal(err)
		}
		runErr := workflow.RunExecution(ctx, exec, wf, dag, reg, store, nil, workflow.NewExpressionEngine(), zerolog.Nop())
		nodes, err := store.GetExecutionNodeOutputs(ctx, exec.ID)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(nodes), runErr
	}

	for _, on := range []bool{false, true} {
		if err := secrets.SetAgentAccess(ctx, db, "default", on); err != nil {
			t.Fatal(err)
		}
		out, err := run()
		if err == nil || strings.Contains(out, "vsecret-777") || strings.Contains(err.Error(), "vsecret-777") {
			t.Errorf("access %v: secret_get of an unapproved workflow: %v\n%s", on, err, out)
		}
		if on && !strings.Contains(err.Error(), "agent access") {
			t.Errorf("the refusal under access does not say why: %v", err)
		}
	}
	// Approved, it reads as before.
	if err := store.ApproveDefinition(ctx, id); err != nil {
		t.Fatal(err)
	}
	if out, err := run(); err != nil || !strings.Contains(out, "vsecret-777") {
		t.Errorf("approved: %v\n%s", err, out)
	}
}
