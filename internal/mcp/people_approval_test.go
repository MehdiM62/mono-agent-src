package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

// person_upsert is an agent's: it never approves a person for outreach
// (the chat's upsert_person, #831 S5), and queueing stays open.
func TestPersonUpsertNeverApproves(t *testing.T) {
	s := newTestServerAllowMutations(t, true)
	ctx := context.Background()
	if _, err := callToolDefault(ctx, s, "person_upsert",
		json.RawMessage(`{"platform":"linkedin","platform_username":"ada","category":"approved","introduction":"Hi Ada"}`)); err == nil ||
		!strings.Contains(err.Error(), "only the user approves") {
		t.Fatalf("approve through MCP: %v", err)
	}
	if _, err := callToolDefault(ctx, s, "person_upsert",
		json.RawMessage(`{"platform":"linkedin","platform_username":"bob","category":"pending_approval","introduction":"Hi Bob"}`)); err != nil {
		t.Fatalf("queue through MCP: %v", err)
	}
	db, err := storage.NewDatabase(s.opts.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM people WHERE platform_username = 'ada'`).Scan(&n)
	var cat string
	db.DB.QueryRow(`SELECT category FROM people WHERE platform_username = 'bob'`).Scan(&cat)
	if n != 0 || cat != "pending_approval" {
		t.Fatalf("ada rows %d, bob %q", n, cat)
	}
}
