package storage

import (
	"context"
	"path/filepath"
	"testing"
)

// A database from before the access column: its coder conversations come out
// full (the access they always had) and the rest scoped, once. A later open
// never runs the backfill again, so a level the operator lowered stays.
func TestChatAccessColumnIsBackfilledOnceFromMode(t *testing.T) {
	d, err := NewDatabase(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	for _, q := range []string{
		`CREATE TABLE ai_chat_conversations (id TEXT PRIMARY KEY, profile_id TEXT NOT NULL, mode TEXT NOT NULL DEFAULT 'assistant')`,
		`INSERT INTO ai_chat_conversations (id, profile_id, mode) VALUES ('a', 'p', 'assistant'), ('c', 'p', 'coder')`,
	} {
		if _, err := d.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	access := func(id string) string {
		var a string
		if err := d.DB.QueryRow(`SELECT access FROM ai_chat_conversations WHERE id = ?`, id).Scan(&a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	if err := d.ReconcileSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if access("a") != "scoped" || access("c") != "full" {
		t.Fatalf("backfill: assistant %q, coder %q", access("a"), access("c"))
	}
	if _, err := d.DB.Exec(`UPDATE ai_chat_conversations SET access = 'scoped' WHERE id = 'c'`); err != nil {
		t.Fatal(err)
	}
	if err := d.ReconcileSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if access("c") != "scoped" {
		t.Errorf("a reopened database raised a lowered conversation back to %q", access("c"))
	}
	if _, err := d.DB.Exec(`UPDATE ai_chat_conversations SET access = 'root' WHERE id = 'a'`); err == nil {
		t.Error("the column accepts a level that does not exist")
	}
}
