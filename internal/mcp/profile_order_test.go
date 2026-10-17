package mcp

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestProfileIDsOldestFirstAcrossMixedCreatedFormats(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
	ids, err := profileIDsOldestFirst(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ids, ""); got != "maz" {
		t.Fatalf("order = %q, want maz", got)
	}
}
