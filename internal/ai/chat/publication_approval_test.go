package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/publication"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// sendApprovalsOff turns the user's approval of sends off, for tests about
// something else that register publications.
func sendApprovalsOff(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO configs (name, config_data) VALUES ('coder_mode', '{"sendApproval":false}')`); err != nil {
		t.Fatal(err)
	}
}

// sentSendIn queues a send as an agent in profile and has the user approve
// it; the send runs and is sent. It returns its id.
func sentSendIn(t *testing.T, db *sql.DB, profile string) string {
	t.Helper()
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db), profile)
	t.Setenv(hilsend.EnvAgentContext, "coder")
	out, handled, err := hilsend.Guard(ctx, "comm.email_send", workflow.NodeInput{}, map[string]interface{}{
		"smtp_host": "smtp.example.test", "from": "me@example.test", "to": "ada@example.test", "subject": "Hello", "body": "Hi"})
	t.Setenv(hilsend.EnvAgentContext, "")
	if err != nil || !handled {
		t.Fatalf("queue a send: handled %v, %v", handled, err)
	}
	id := out[0].Items[0].JSON["hil_id"].(string)
	sent := func(context.Context, hilsend.Run) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	}
	if _, err := hilsend.Resolve(ctx, hilsend.Store{DB: db, Profile: profile}, id, true, hilsend.Edit{}, false, sent); err != nil {
		t.Fatal(err)
	}
	return id
}

// The chat is not the operator: what it registers belongs to a send the user
// approved and that was sent, as `publication register` asks of an agent,
// while the user's approval of sends is on (#831 F2).
func TestRegisterPublicationNeedsAnApprovedSend(t *testing.T) {
	db := newMonoagentTestDB(t)
	mt := NewMonoagentTools(db.DB, "", "default")
	ctx := context.Background()
	entry := `{"platform":"email","kind":"article","title":"Our article"`
	for _, approval := range []string{"", `,"approval":"missing"`} {
		if _, err := mt.ExecuteContext(ctx, "register_publication", entry+approval+`}`); err == nil ||
			!strings.Contains(err.Error(), "needs an approved send") || !strings.Contains(err.Error(), "approval") {
			t.Errorf("approval %q: %v", approval, err)
		}
	}
	if list, err := publication.NewStore(db.DB, "default").List(ctx, publication.Filter{}); err != nil || len(list) != 0 {
		t.Fatalf("registered without approval: %v %v", list, err)
	}
	id := sentSendIn(t, db.DB, "default")
	if _, err := mt.ExecuteContext(ctx, "register_publication", `{"platform":"blog","kind":"article","title":"Our article","approval":"`+id+`"}`); err == nil ||
		!strings.Contains(err.Error(), "not the publication's") {
		t.Errorf("a publication on another platform: %v", err)
	}
	if _, err := mt.ExecuteContext(ctx, "register_publication", entry+`,"approval":"`+id+`"}`); err != nil {
		t.Errorf("with a sent send: %v", err)
	}
	if _, err := mt.ExecuteContext(ctx, "register_publication", entry+`,"approval":"`+id+`"}`); err == nil || !strings.Contains(err.Error(), "already registered a publication") {
		t.Errorf("the approval again: %v", err)
	}
	sendApprovalsOff(t, db.DB)
	if _, err := mt.ExecuteContext(ctx, "register_publication", entry+`}`); err != nil {
		t.Errorf("with approvals of sends off: %v", err)
	}
	if list, _ := publication.NewStore(db.DB, "default").List(ctx, publication.Filter{}); len(list) != 2 {
		t.Errorf("%d publications, want 2", len(list))
	}
}
