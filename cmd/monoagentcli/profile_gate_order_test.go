package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// created_at is stored in two formats; profiles made in the same second must
// list oldest first, then by id, not by the character between date and time.
func TestGateProfileListOrdersMixedCreatedFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gate.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT, created_at TEXT)`,
		`INSERT INTO profiles VALUES ('z', 'Zed', '2026-10-16 06:04:33')`,
		`INSERT INTO profiles VALUES ('a', 'Ay', '2026-10-16T06:04:33Z')`,
		`INSERT INTO profiles VALUES ('m', 'Em', '2026-10-16T06:04:32Z')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	refs, err := listProfilesReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, r := range refs {
		got += r.ID
	}
	if got != "maz" {
		t.Fatalf("order = %q, want maz", got)
	}
}
