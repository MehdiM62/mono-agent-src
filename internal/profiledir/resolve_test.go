package profiledir

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func resolveFixture(t *testing.T, rows ...[3]string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE profiles (id TEXT PRIMARY KEY, name TEXT, created_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO profiles (id, name, created_at) VALUES (?, ?, ?)`, r[0], r[1], r[2]); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	db := resolveFixture(t,
		[3]string{"a1", "Work", "2026-01-01"},
		[3]string{"b2", "Home", "2026-01-02"},
		[3]string{"c3", "home", "2026-01-03"},
		[3]string{"_machine", "Machine", "2026-01-04"},
		[3]string{"d4", "work", "2026-01-05"},
	)
	if got, err := Resolve(ctx, db, "a1"); err != nil || got != "a1" {
		t.Errorf("by id: %q, %v", got, err)
	}
	if got, err := Resolve(ctx, db, "d4"); err != nil || got != "d4" {
		t.Errorf("exact id wins over a name: %q, %v", got, err)
	}
	if got, err := Resolve(ctx, db, "MACHINE"); err != nil || got != "_machine" {
		t.Errorf("a name that is _machine's name still resolves by name: %q, %v", got, err)
	}
	if _, err := Resolve(ctx, db, "_machine"); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("_machine is never an id: %v", err)
	}
	if _, err := Resolve(ctx, db, "nope"); !errors.Is(err, ErrUnknownProfile) {
		t.Errorf("unknown: %v", err)
	}
	_, err := Resolve(ctx, db, "HOME")
	var amb *AmbiguousError
	if !errors.Is(err, ErrAmbiguousProfile) || !errors.As(err, &amb) || len(amb.IDs) != 2 || amb.IDs[0] != "b2" {
		t.Errorf("ambiguous name: %v", err)
	}
	if ids, err := Candidates(ctx, db, "work"); err != nil || len(ids) != 2 {
		t.Errorf("Candidates(work) = %v, %v", ids, err)
	}
	if ids, err := Candidates(ctx, db, ""); err != nil || len(ids) != 0 {
		t.Errorf("Candidates(empty) = %v, %v", ids, err)
	}
	if got := Describe(ctx, db); got != "Work (a1), Home (b2), home (c3), Machine (_machine), work (d4)" {
		t.Errorf("Describe = %q", got)
	}
}
