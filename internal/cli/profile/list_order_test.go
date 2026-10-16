package profile

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestProfileListOrdersMixedCreatedFormats(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`DELETE FROM profiles`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('z', 'Zed', '2026-10-16 06:04:33')`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('a', 'Ay', '2026-10-16T06:04:33Z')`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('m', 'Em', '2026-10-16T06:04:32Z')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	var got []struct{ ID string }
	mustProfileJSON(t, dbPath, &got, "list")
	var order []string
	for _, p := range got {
		order = append(order, p.ID)
	}
	if s := strings.Join(order, ""); s != "maz" {
		t.Fatalf("order = %q, want maz", s)
	}
}
