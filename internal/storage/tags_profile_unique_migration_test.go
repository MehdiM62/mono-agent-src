package storage

import (
	"path/filepath"
	"testing"
)

// Migration 100: tag names were unique across the whole machine
// (name TEXT UNIQUE), so two profiles could not both own a tag called
// "lead". The rebuild makes the name unique per profile, keeps every tag id
// (so people_tags links survive) and leaves one profile's duplicates
// impossible.
func TestApplyMigration100_TagNamesArePerProfile(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "tags.db"))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.DB.Close()
	applyMigrationsBelow(t, db, 100)

	for _, stmt := range []string{
		`INSERT INTO people (id, platform_username, platform, profile_id) VALUES ('pa', 'a', 'X', 'A'), ('pb', 'b', 'X', 'B')`,
		`INSERT INTO tags (id, name, color, profile_id) VALUES ('t-a', 'lead', '#ff0000', 'A'), ('t-b', 'Warm', '#00ff00', 'B')`,
		`INSERT INTO people_tags (person_id, tag_id) VALUES ('pa', 't-a'), ('pb', 't-b')`,
	} {
		if _, err := db.DB.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	// Before the migration the name is globally unique.
	if _, err := db.DB.Exec(`INSERT INTO tags (id, name, profile_id) VALUES ('t-b2', 'LEAD', 'B')`); err == nil {
		t.Fatal("setup: expected the old global UNIQUE(name) to refuse a second 'lead'")
	}

	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	var color string
	if err := db.DB.QueryRow(`SELECT color FROM tags WHERE id = 't-a' AND name = 'lead' AND profile_id = 'A'`).Scan(&color); err != nil || color != "#ff0000" {
		t.Fatalf("tag t-a after rebuild: %q, %v", color, err)
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM people_tags pt JOIN tags t ON t.id = pt.tag_id`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("people_tags links resolving to a tag = %d (%v), want 2", n, err)
	}

	// Profile B can now own 'lead' too (any case); A still cannot repeat it.
	if _, err := db.DB.Exec(`INSERT INTO tags (id, name, profile_id) VALUES ('t-b2', 'LEAD', 'B')`); err != nil {
		t.Fatalf("second profile's 'lead': %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO tags (id, name, profile_id) VALUES ('t-a2', 'Lead', 'A')`); err == nil {
		t.Fatal("a profile must not own two tags that differ only in case")
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("re-applying: %v", err)
	}
}
