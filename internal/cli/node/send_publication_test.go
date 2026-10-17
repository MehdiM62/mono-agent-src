package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/publication"
	"github.com/monoes/mono-agent/internal/workflow"
)

// fakeDevto is service.devto publishing nothing: every article it "publishes"
// is article 4242.
type fakeDevto struct{}

func (fakeDevto) Type() string { return "service.devto" }

func (fakeDevto) Execute(context.Context, workflow.NodeInput, map[string]interface{}) ([]workflow.NodeOutput, error) {
	return []workflow.NodeOutput{{Handle: "main", Items: []workflow.Item{{JSON: map[string]interface{}{
		"id": "4242", "url": "https://dev.to/me/hello-4242"}}}}}, nil
}

// An approved send's own run (`node run` with the approval's environment, as
// `monoagentcli hil` runs it) registers the publication it made and spends
// the approval on it: no other publication can be registered with it after
// (#831 round 4, S7).
func TestAnApprovedSendsRunSpendsItsApproval(t *testing.T) {
	for _, k := range append(orgsign.AgentContextMarkers(), hilsend.EnvAgentContext, "MONOAGENT_ACTOR", browserctl.EnvAICaller, workflow.EnvWorkflowRun,
		hilsend.EnvApprovalID, hilsend.EnvApprovalProof, hilsend.EnvConversation, hilsend.EnvAgentRole, hilsend.EnvAgentID) {
		t.Setenv(k, "")
	}
	d := testDeps(nodeVaultTestDB(t), "default", false)
	d.BuildRegistry = func(*sql.DB) *workflow.NodeTypeRegistry {
		workflow.SetExecGuard(hilsend.Guard) // as noderegistry.Build does
		r := workflow.NewNodeTypeRegistry()
		r.Register("service.devto", func() workflow.NodeExecutor { return fakeDevto{} })
		return r
	}
	config := map[string]interface{}{"operation": "publish_article", "title": "Hello", "body_markdown": "Hi all", "api_key": "k"}
	payload := func(config map[string]interface{}, input []map[string]interface{}) string {
		items := []map[string]interface{}{}
		for _, in := range input {
			items = append(items, map[string]interface{}{"json": in})
		}
		raw, err := json.Marshal(map[string]interface{}{"config": config, "input": items})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	// An agent's article is queued for approval.
	t.Setenv(hilsend.EnvAgentContext, "coder")
	out, err := runNodeSubcmdStdin(t, d, payload(config, nil), "service.devto")
	if err != nil {
		t.Fatalf("agent node run: %v\n%s", err, out)
	}
	var queued map[string][]map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(out), &queued); err != nil || len(queued["main"]) != 1 {
		t.Fatalf("agent node run output: %v\n%s", err, out)
	}
	id, _ := queued["main"][0]["json"]["hil_id"].(string)
	if id == "" {
		t.Fatalf("not queued: %s", out)
	}

	// The operator approves it, and the send runs.
	t.Setenv(hilsend.EnvAgentContext, "")
	db, err := d.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	run := func(_ context.Context, r hilsend.Run) (json.RawMessage, error) {
		t.Setenv(hilsend.EnvApprovalID, r.ApprovalID)
		t.Setenv(hilsend.EnvApprovalProof, r.Proof)
		defer t.Setenv(hilsend.EnvApprovalID, "")
		defer t.Setenv(hilsend.EnvApprovalProof, "")
		out, err := runNodeSubcmdStdin(t, d, payload(r.Config, r.Input), r.NodeType)
		return json.RawMessage(out), err
	}
	if p, err := hilsend.Resolve(ctx, hilsend.Store{DB: db.DB, Profile: "default"}, id, true, hilsend.Edit{}, false, run); err != nil || p.State != hilsend.StateSent {
		t.Fatalf("approve: %v %+v", err, p)
	}
	recorded, err := publication.NewStore(db.DB, "default").List(ctx, publication.Filter{})
	if err != nil || len(recorded) != 1 || recorded[0].RemoteID != "4242" {
		t.Fatalf("the send's publication: %v %+v", err, recorded)
	}

	// Another publication with the approval is refused; the send's own again
	// is a retry, and passes.
	t.Setenv(hilsend.EnvAgentContext, "coder")
	another := publication.Entry{Platform: "devto", Kind: "post", Body: "Something else", RemoteID: "4242"}
	if _, err := hilsend.ClaimPublicationApproval(ctx, db.DB, "default", id, another); !errors.Is(err, hilsend.ErrPublicationApprovalUsed) {
		t.Errorf("another publication with an approval its send spent: %v", err)
	}
	if claim, err := hilsend.ClaimPublicationApproval(ctx, db.DB, "default", id, recorded[0]); err != nil || claim != nil {
		t.Errorf("the send's own publication again: %v %v", claim, err)
	}
}
