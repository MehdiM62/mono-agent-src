package storage

import (
	"path/filepath"
	"testing"
)

// Migration 102 adds person_messages.hil_ref and people.kind. Rows that exist
// keep their meaning: a synced message stays 'sent' with no hil_ref, a person
// stays a person, and the listings that were there still return them.
func TestApplyMigration102_CommsStatus(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "upgraded.db"))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.DB.Close()
	applyMigrationsBelow(t, db, 102)

	if _, err := db.DB.Exec(`INSERT INTO people (id, platform_username, platform, full_name, category, introduction, profile_id)
		VALUES ('p', 'ada@example.test', 'email', 'Ada', 'approved', 'draft dm', 'default')`); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO person_messages (id, person_id, source, external_id, direction, subject, profile_id)
		VALUES ('m', 'p', 'outlook', 'x1', 'inbound', 'Hello', 'default')`); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	var status, hilRef, kind, category, intro string
	if err := db.DB.QueryRow(`SELECT status, COALESCE(hil_ref,'') FROM person_messages WHERE id = 'm'`).Scan(&status, &hilRef); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || hilRef != "" {
		t.Errorf("existing message = %q / %q, want sent / none", status, hilRef)
	}
	if err := db.DB.QueryRow(`SELECT kind, category, introduction FROM people WHERE id = 'p'`).Scan(&kind, &category, &intro); err != nil {
		t.Fatal(err)
	}
	if kind != "person" || category != "approved" || intro != "draft dm" {
		t.Errorf("existing person = %q / %q / %q", kind, category, intro)
	}

	msgs, err := db.ListAllPersonMessagesByStatus("default", "", "", false, 10, 0)
	if err != nil || len(msgs) != 1 || msgs[0].Status != "sent" || msgs[0].HilRef != "" {
		t.Fatalf("listing = %v %+v", err, msgs)
	}
	if only, err := db.ListAllPersonMessagesByStatus("default", "", "pending_approval", false, 10, 0); err != nil || len(only) != 0 {
		t.Fatalf("status filter = %v %+v", err, only)
	}

	// The index exists and a second run changes nothing.
	var idx string
	if err := db.DB.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_person_messages_hil_ref'`).Scan(&idx); err != nil {
		t.Errorf("hil_ref index missing: %v", err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("second ApplyMigrations: %v", err)
	}
}
