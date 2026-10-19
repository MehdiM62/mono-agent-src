package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/workflow"
)

// sentNode is a sending node that sends nothing: it counts its runs.
type sentNode struct {
	typ  string
	runs *atomic.Int32
}

func (n sentNode) Type() string { return n.typ }

func (n sentNode) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	n.runs.Add(1)
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{{JSON: map[string]interface{}{"sent": true}}}}}, nil
}

// An agent's `node run` of a send under a legacy unprefixed name (an alias
// of the registry) waits for approval as under its canonical name. The
// guard was asked with the name as typed, which no send rule knew, so
// `node run telegram …` sent with no card.
func TestNodeRunOfALegacyNameWaits(t *testing.T) {
	for _, k := range append(orgsign.AgentContextMarkers(), hilsend.EnvAgentContext, "MONOAGENT_ACTOR", browserctl.EnvAICaller, workflow.EnvWorkflowRun,
		hilsend.EnvApprovalID, hilsend.EnvApprovalProof, hilsend.EnvConversation, hilsend.EnvAgentRole, hilsend.EnvAgentID) {
		t.Setenv(k, "")
	}
	var runs atomic.Int32
	d := testDeps(nodeVaultTestDB(t), "default", false)
	d.BuildRegistry = func(*sql.DB) *workflow.NodeTypeRegistry {
		workflow.SetExecGuard(hilsend.Guard) // as noderegistry.Build does
		r := workflow.NewNodeTypeRegistry()
		for legacy, canonical := range map[string]string{"telegram": "comm.telegram", "gmail": "service.gmail", "discord": "service.discord"} {
			typ := canonical
			r.Register(typ, func() workflow.NodeExecutor { return sentNode{typ: typ, runs: &runs} })
			r.Alias(legacy, canonical) // as noderegistry.Build does
		}
		return r
	}
	t.Setenv(hilsend.EnvAgentContext, "coder")
	for name, config := range map[string]map[string]interface{}{
		"telegram": {"chat_id": "42", "text": "Hi Ada"},
		"gmail":    {"operation": "send_message", "to": "ada@example.test", "subject": "Hello", "body": "Hi Ada"},
		"discord":  {"operation": "send_message", "channel_id": "C1", "content": "Hi all"},
	} {
		raw, _ := json.Marshal(map[string]interface{}{"config": config})
		out, err := runNodeSubcmdStdin(t, d, string(raw), name)
		if err != nil {
			t.Errorf("node run %s: %v\n%s", name, err, out)
			continue
		}
		var queued map[string][]map[string]map[string]interface{}
		if err := json.Unmarshal([]byte(out), &queued); err != nil || len(queued["main"]) != 1 || queued["main"][0]["json"]["hil_id"] == nil {
			t.Errorf("node run %s was not queued: %v\n%s", name, err, out)
		}
	}
	if n := runs.Load(); n != 0 {
		t.Errorf("%d of the sends ran without an approval", n)
	}
}
