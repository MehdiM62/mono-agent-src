package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/publication"
)

// sendApprovalsOff turns the user's approval of sends off, for tests about
// something else that register publications.
func sendApprovalsOff(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO configs (name, config_data) VALUES ('coder_mode', '{"sendApproval":false}')`); err != nil {
		t.Fatal(err)
	}
}

// The server is a path an agent reaches: publication_register asks for a
// send the user approved, as `publication register` asks of an agent, while
// the user's approval of sends is on (#831 F2).
func TestPublicationRegisterToolNeedsAnApprovedSend(t *testing.T) {
	s := newTestServerAllowMutations(t, true)
	rt, err := s.runtime()
	if err != nil {
		t.Fatal(err)
	}
	defer s.closeRuntime()
	ctx := context.Background()
	entry := json.RawMessage(`{"platform":"custom","kind":"post","body":"ours","approval":"missing"}`)
	if _, err := callToolDefault(ctx, s, "publication_register", entry); err == nil || !strings.Contains(err.Error(), "needs an approved send") {
		t.Fatalf("without a sent send: %v", err)
	}
	if list, err := publication.NewStore(rt.db.DB, testProfile).List(ctx, publication.Filter{}); err != nil || len(list) != 0 {
		t.Fatalf("registered without approval: %v %v", list, err)
	}
	sendApprovalsOff(t, rt.db.DB)
	if _, err := callToolDefault(ctx, s, "publication_register", entry); err != nil {
		t.Errorf("with approvals of sends off: %v", err)
	}
}
