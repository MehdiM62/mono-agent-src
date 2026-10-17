package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/vault"
)

// configProbe hands on the config it ran with.
type configProbe struct{ got *[]map[string]interface{} }

func (configProbe) Type() string { return "test.configprobe" }
func (p configProbe) Execute(_ context.Context, _ NodeInput, config map[string]interface{}) ([]NodeOutput, error) {
	*p.got = append(*p.got, config)
	return []NodeOutput{{Handle: "main", Items: []Item{NewItem(config)}}}, nil
}

// A webhook's body reaches an approved workflow's fields through its
// expressions, and a @secret reference in it resolves nowhere: not as the
// whole value, not built by the expression from pieces, not inside a header.
// The step is refused, naming the field, and the value is in no error. The
// workflow's own reference, in the same step, resolves once the data holds
// none (#845 review: secret references as data).
func TestASecretReferenceInAWebhooksBodyResolvesNowhere(t *testing.T) {
	keyring.MockInit()
	db, sqlStore := approvalsTestDB(t)
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db.DB), "default")
	if _, err := secrets.Add(ctx, db.DB, "default", "secret", "vs", map[string]string{"secret": "vsecret-777"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		field string
		body  map[string]interface{}
	}{
		"the whole value":          {"{{ $json.body.msg }}", map[string]interface{}{"msg": "@secret:vs"}},
		"built from two pieces":    {"{{ $json.body.a }}{{ $json.body.b }}", map[string]interface{}{"a": "@sec", "b": "ret:vs"}},
		"built by the expression":  {`@sec{{ "ret:" }}{{ $json.body.name }}`, map[string]interface{}{"name": "vs"}},
		"inside a longer value":    {"Bearer {{ $json.body.msg }}", map[string]interface{}{"msg": "@secret:vs"}},
		"in an object it returned": {"{{ json $json.body.values }}", map[string]interface{}{"values": map[string]interface{}{"pin": "@secret:vs"}}},
	} {
		var got []map[string]interface{}
		err := runHookedWorkflow(t, ctx, sqlStore, &got, map[string]interface{}{"own": "@secret:vs", "msg": c.field}, c.body)
		if !errors.Is(err, secrets.ErrDataReference) || !strings.Contains(err.Error(), `"msg`) {
			t.Errorf("%s: %v, want the step refused for a reference in msg that came from data", name, err)
		}
		if strings.Contains(fmt.Sprint(err, got), "vsecret-777") || len(got) != 0 {
			t.Errorf("%s: the step ran with %v (%v)", name, got, err)
		}
	}

	var got []map[string]interface{}
	if err := runHookedWorkflow(t, ctx, sqlStore, &got, map[string]interface{}{"own": "@secret:vs", "msg": "{{ $json.body.msg }}"}, map[string]interface{}{"msg": "hello"}); err != nil {
		t.Fatalf("a body without a reference: %v", err)
	}
	if len(got) != 1 || got[0]["own"] != "vsecret-777" || got[0]["msg"] != "hello" {
		t.Errorf("the approved workflow's own reference: the step ran with %v", got)
	}
}

// runHookedWorkflow runs an approved workflow whose webhook body is body and
// whose probe step has config.
func runHookedWorkflow(t *testing.T, ctx context.Context, store *SQLiteWorkflowStore, got *[]map[string]interface{}, config, body map[string]interface{}) error {
	t.Helper()
	id := "wf-" + uuid.NewString()[:8]
	reg := NewNodeTypeRegistry()
	reg.Register("test.configprobe", func() NodeExecutor { return configProbe{got} })
	wf := &Workflow{ID: id, Name: id, ProfileID: "default",
		Nodes: []WorkflowNode{
			{ID: id + "-t", WorkflowID: id, Type: "trigger.webhook", Name: "Hook"},
			{ID: id + "-p", WorkflowID: id, Type: "test.configprobe", Name: "Probe", Config: config},
		},
		Connections: []WorkflowConnection{{ID: id + "-c", WorkflowID: id, SourceNodeID: id + "-t", SourceHandle: "main", TargetNodeID: id + "-p", TargetHandle: "main"}},
	}
	createSQLWorkflow(t, store, wf)
	if err := store.ApproveDefinition(ctx, wf.ID); err != nil {
		t.Fatal(err)
	}
	dag, err := BuildDAG(wf.Nodes, wf.Connections)
	if err != nil {
		t.Fatal(err)
	}
	exec := &WorkflowExecution{ID: uuid.NewString(), WorkflowID: wf.ID, ProfileID: "default", TriggerType: "trigger.webhook", TriggerNodeID: id + "-t",
		TriggerData: map[string]interface{}{"body": body, "headers": map[string]interface{}{}}}
	return RunExecution(ctx, exec, wf, dag, reg, store, nil, NewExpressionEngine(), zerolog.Nop())
}
