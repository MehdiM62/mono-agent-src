package mcp

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/workflow"
)

// aliasSendNode is a sending node that sends nothing: it counts its runs.
type aliasSendNode struct{ runs *atomic.Int32 }

func (aliasSendNode) Type() string { return "comm.telegram" }

func (n aliasSendNode) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	n.runs.Add(1)
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{{JSON: map[string]interface{}{"sent": true}}}}}, nil
}

// node_run of a send under a legacy unprefixed name waits for approval as
// under its canonical name: the guard was asked with the name as given,
// which no send rule knew, so node_run "telegram" sent with no card.
func TestNodeRunToolOfALegacyNameWaits(t *testing.T) {
	for _, m := range append(orgsign.AgentContextMarkers(), hilsend.EnvAgentContext, hilsend.EnvApprovalID, hilsend.EnvApprovalProof) {
		t.Setenv(m, "")
	}
	s := newTestServerAllowMutations(t, true)
	rt, err := s.runtime()
	if err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int32
	r := workflow.NewNodeTypeRegistry()
	r.Register("comm.telegram", func() workflow.NodeExecutor { return aliasSendNode{runs: &runs} })
	r.Alias("telegram", "comm.telegram") // as noderegistry.Build does
	rt.registry = r                      // ensureEngine keeps it (and installs the guard)
	out, err := callNamed(t, s, "node_run", map[string]interface{}{
		"type": "telegram", "config": map[string]interface{}{"chat_id": "42", "text": "Hi Ada"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runs.Load() != 0 || !strings.Contains(out, `"hil_id"`) || !strings.Contains(out, "queued for approval") {
		t.Fatalf("node_run telegram ran %d time(s): %s", runs.Load(), out)
	}
}
