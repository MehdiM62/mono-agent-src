package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLegacyOwnerProfileOrdersMixedCreatedFormats(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "o.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`DELETE FROM profiles`); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`INSERT INTO profiles (id, name, created_at) VALUES ('z', 'Zed', '2026-10-16 06:04:33')`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('a', 'Ay', '2026-10-16T06:04:33Z')`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('m', 'Em', '2026-10-16T06:04:32Z')`,
	} {
		if _, err := db.DB.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.LegacyOwnerProfile(context.Background())
	if err != nil || got != "m" {
		t.Fatalf("LegacyOwnerProfile = %q, %v; want m", got, err)
	}
	if _, err := db.DB.Exec(`INSERT INTO profiles (id, name, created_at) VALUES ('default', 'Default', '2026-10-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.LegacyOwnerProfile(context.Background()); got != "default" {
		t.Fatalf("LegacyOwnerProfile = %q; the default profile always wins", got)
	}
}
