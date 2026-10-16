package profiledir

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestListMarksTheOldestAcrossMixedFormats(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range []string{
		`CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT, created_at TEXT)`,
		// 'T' sorts after ' ', so a text compare would put "work" last even though it is older.
		`INSERT INTO profiles VALUES ('work', 'work', '2026-10-16T06:04:33Z')`,
		`INSERT INTO profiles VALUES ('default', 'Default', '2026-10-16 06:04:34')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	got, err := List(context.Background(), db)
	if err != nil || len(got) != 2 || got[0].ID != "work" || !got[0].Default || got[1].Default {
		t.Fatalf("List = %+v, %v; want work first and marked", got, err)
	}
}
