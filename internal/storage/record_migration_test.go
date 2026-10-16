package storage

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newRecordTestDB(t *testing.T) *Database {
	t.Helper()
	db, err := NewDatabase(filepath.Join(t.TempDir(), "rec.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	if _, err := db.DB.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, filename TEXT)`); err != nil {
		t.Fatal(err)
	}
	return db
}

// Two appliers recording one version without the cross-process lock must
// both succeed and leave exactly one row.
func TestRecordMigrationRacingAppliersLeaveOneRow(t *testing.T) {
	db := newRecordTestDB(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- recordMigration(db.DB, 7, "007_x.sql")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("recording an already-recorded version failed: %v", err)
		}
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 7`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d, err = %v; want exactly 1", n, err)
	}
}

func TestRecordMigrationToleratesAlreadyPresentVersion(t *testing.T) {
	db := newRecordTestDB(t)
	for i := 0; i < 2; i++ {
		if err := recordMigration(db.DB, 3, "003_y.sql"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}

func TestRecordMigrationSQLIgnoresDuplicates(t *testing.T) {
	if !strings.Contains(recordMigrationSQL, "OR IGNORE") {
		t.Fatalf("recordMigrationSQL must be INSERT OR IGNORE, got %q", recordMigrationSQL)
	}
}
