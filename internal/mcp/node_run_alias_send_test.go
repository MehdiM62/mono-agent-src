package mcp

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/monoes/mono-agent/internal/connections"
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

// node_run of a send pins the connection it resolved on the card, by id:
// with credential_id "outlook" and two Outlook connections, the active one,
// so an approval sends through it rather than whichever it would pick then.
func TestNodeRunToolOfASendPinsItsConnection(t *testing.T) {
	for _, m := range append(orgsign.AgentContextMarkers(), hilsend.EnvAgentContext, hilsend.EnvApprovalID, hilsend.EnvApprovalProof) {
		t.Setenv(m, "")
	}
	s := newTestServerAllowMutations(t, true)
	rt, err := s.runtime()
	if err != nil {
		t.Fatal(err)
	}
	profile := serverTestProfile(s)
	store := connections.NewStore(rt.db.DB)
	for _, c := range []*connections.Connection{
		{ID: "conn-old", Platform: "outlook", Method: "oauth", Status: "expired", ProfileID: profile, Data: map[string]interface{}{"email": "me@x.com"}},
		{ID: "conn-new", Platform: "outlook", Method: "oauth", Status: "active", ProfileID: profile, Data: map[string]interface{}{"email": "me@x.com"}},
	} {
		if err := store.Save(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	var runs atomic.Int32
	r := workflow.NewNodeTypeRegistry()
	r.Register("service.outlook_mail", func() workflow.NodeExecutor { return pinSendNode{runs: &runs} })
	rt.registry = r
	out, err := callNamed(t, s, "node_run", map[string]interface{}{
		"type": "service.outlook_mail", "config": map[string]interface{}{"operation": "send_message", "to": "ada@example.test",
			"subject": "Hi", "body": "Hello", "credential_id": "outlook"},
	})
	if err != nil || runs.Load() != 0 || !strings.Contains(out, `"hil_id"`) {
		t.Fatalf("node_run: %v (runs %d)\n%s", err, runs.Load(), out)
	}
	cards, err := hilsend.Store{DB: rt.db.DB, Profile: profile}.List(context.Background())
	if err != nil || len(cards) != 1 {
		t.Fatalf("cards: %v %+v", err, cards)
	}
	for _, d := range cards[0].Details {
		if d.Key == "connection" && d.Value != "conn-new" {
			t.Fatalf("connection %q, want conn-new pinned", d.Value)
		}
	}
}

// pinSendNode is an Outlook send that sends nothing: it counts its runs.
type pinSendNode struct{ runs *atomic.Int32 }

func (pinSendNode) Type() string { return "service.outlook_mail" }

func (n pinSendNode) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	n.runs.Add(1)
	return nil, nil
}
