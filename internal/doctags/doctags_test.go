package doctags

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/peopletags"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testhome"
)

// Tests never touch the real ~/.monoagent.
func TestMain(m *testing.M) { testhome.Main(m) }

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.NewDatabase(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for i, d := range [][2]string{{"d1", "prof"}, {"d2", "prof"}, {"dx", "other"}} {
		if _, err := db.DB.Exec(`INSERT INTO vault_documents (id, seq, path, filename, profile_id, created_at) VALUES (?,?,?,?,?,'now')`,
			d[0], i+1, "/tmp/"+d[0], d[0], d[1]); err != nil {
			t.Fatal(err)
		}
	}
	return db.DB
}

func TestAddSearchRemove(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	a, err := Add(ctx, db, "prof", "d1", " Contract ")
	if err != nil || a.Name != "Contract" || a.ID == "" {
		t.Fatalf("Add = %+v, %v", a, err)
	}
	// Same name in any case is the same tag; relinking is a no-op.
	b, err := Add(ctx, db, "prof", "d2", "CONTRACT")
	if err != nil || b.ID != a.ID {
		t.Fatalf("second Add = %+v, %v", b, err)
	}
	if _, err := Add(ctx, db, "prof", "d1", "contract"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(ctx, db, "prof", "d1", "2026"); err != nil {
		t.Fatal(err)
	}

	tags, _ := List(ctx, db, "prof", "d1")
	if len(tags) != 2 {
		t.Fatalf("d1 tags = %+v", tags)
	}
	if got, _ := Search(ctx, db, "prof", []string{"contract"}); len(got) != 2 {
		t.Fatalf("search contract = %v", got)
	}
	// Several tags: documents with all of them (a repeated tag counts once).
	got, _ := Search(ctx, db, "prof", []string{"contract", "2026", "CONTRACT"})
	if len(got) != 1 || got[0] != "d1" {
		t.Fatalf("search contract+2026 = %v", got)
	}
	if got, err := Search(ctx, db, "prof", []string{"nope"}); err != nil || len(got) != 0 {
		t.Fatalf("unknown tag = %v, %v", got, err)
	}

	if err := Remove(ctx, db, "prof", "d1", "2026"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Search(ctx, db, "prof", []string{"2026"}); len(got) != 0 {
		t.Fatalf("after remove = %v", got)
	}
}

func TestScopedToProfile(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if _, err := Add(ctx, db, "prof", "dx", "x"); !errors.Is(err, peopletags.ErrNotFound) {
		t.Fatalf("other profile's document: %v", err)
	}
	if _, err := Add(ctx, db, "other", "dx", "x"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Search(ctx, db, "prof", []string{"x"}); len(got) != 0 {
		t.Fatalf("leaked across profiles: %v", got)
	}
}

func TestLimitsAndValidation(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if _, err := Add(ctx, db, "prof", "d1", "  "); !errors.Is(err, peopletags.ErrInvalid) {
		t.Fatalf("empty name: %v", err)
	}
	for i := 0; i < MaxPerDocument; i++ {
		if _, err := Add(ctx, db, "prof", "d1", fmt.Sprintf("t%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Add(ctx, db, "prof", "d1", "one-more"); !errors.Is(err, peopletags.ErrInvalid) {
		t.Fatalf("over the cap: %v", err)
	}
	// An already-linked tag is still fine at the cap.
	if _, err := Add(ctx, db, "prof", "d1", "t0"); err != nil {
		t.Fatalf("relink at cap: %v", err)
	}
}

func TestDeletingADocumentDropsItsLinks(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	if _, err := Add(ctx, db, "prof", "d1", "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM vault_documents WHERE id = 'd1'`); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM document_tags`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d links survived the document", n)
	}
}
