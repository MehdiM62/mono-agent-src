package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// openWithData opens a database at dir/monoagent.db with every migration
// but the newest applied and a user row, so ApplyMigrations has exactly one
// pending migration to run on a database that holds data.
func openWithData(t *testing.T, dir string) (*Database, int) {
	t.Helper()
	db, err := NewDatabase(filepath.Join(dir, "monoagent.db"))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	migs, err := embeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	newest := highestEmbeddedVersion(migs)
	applyMigrationsBelow(t, db, newest)
	if _, err := db.DB.Exec(`CREATE TABLE keepme (v TEXT); INSERT INTO keepme VALUES ('precious')`); err != nil {
		t.Fatal(err)
	}
	return db, newest
}

func listSnapshots(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "backups"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "pre-migrate-") && strings.HasSuffix(e.Name(), ".db") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func captureMigrateStderr(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := migrateStderr
	migrateStderr = &buf
	t.Cleanup(func() { migrateStderr = prev })
	return &buf
}

func TestApplyMigrationsSnapshotsBeforePending(t *testing.T) {
	captureMigrateStderr(t)
	dir := t.TempDir()
	db, newest := openWithData(t, dir)

	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	snaps := listSnapshots(t, dir)
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %v, want exactly one", snaps)
	}
	migs, _ := embeddedMigrations()
	if want := fmt.Sprintf("-v%03d.db", migs[len(migs)-2].version); !strings.HasSuffix(snaps[0], want) {
		t.Errorf("snapshot %s, want the last applied version in its name (%s)", snaps[0], want)
	}
	path := filepath.Join(dir, "backups", snaps[0])
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no Unix permission bits (it reports 0666 for any file).
	if perm := info.Mode().Perm(); runtime.GOOS != "windows" && perm != 0o600 {
		t.Errorf("snapshot mode = %o, want 600", perm)
	}

	// The snapshot is the pre-migration database: it has the data and
	// lacks the newest migration's record.
	snap, err := NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	var v string
	if err := snap.DB.QueryRow(`SELECT v FROM keepme`).Scan(&v); err != nil || v != "precious" {
		t.Fatalf("snapshot keepme = %q, %v", v, err)
	}
	var n int
	snap.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, newest).Scan(&n)
	if n != 0 {
		t.Errorf("snapshot already records v%d — it was taken after migrating", newest)
	}

	// Nothing pending now: a second run takes no snapshot.
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	if got := listSnapshots(t, dir); len(got) != 1 {
		t.Errorf("up-to-date database snapshotted again: %v", got)
	}
}

func TestApplyMigrationsNoSnapshotOnFreshDatabase(t *testing.T) {
	captureMigrateStderr(t)
	dir := t.TempDir()
	db, err := NewDatabase(filepath.Join(dir, "monoagent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backups")); !os.IsNotExist(err) {
		t.Errorf("fresh database created a backups folder (err=%v): %v", err, listSnapshots(t, dir))
	}
}

func TestApplyMigrationsSnapshotRetentionKeepsThree(t *testing.T) {
	captureMigrateStderr(t)
	dir := t.TempDir()
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	old := []string{
		"pre-migrate-20200101T000000-v001.db",
		"pre-migrate-20200102T000000-v002.db",
		"pre-migrate-20200103T000000-v003.db",
		"pre-migrate-20200104T000000-v004.db",
	}
	for _, n := range append(old, "retired-ai-providers.json", "manual.db") {
		if err := os.WriteFile(filepath.Join(backups, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prev := migrateNow
	migrateNow = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { migrateNow = prev })

	db, _ := openWithData(t, dir)
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	got := listSnapshots(t, dir)
	if len(got) != 3 {
		t.Fatalf("snapshots = %v, want 3", got)
	}
	if got[0] != old[2] || got[1] != old[3] || !strings.HasPrefix(got[2], "pre-migrate-20261013T120000-v") {
		t.Errorf("kept %v, want the two newest old ones plus today's", got)
	}
	for _, n := range []string{"retired-ai-providers.json", "manual.db"} {
		if _, err := os.Stat(filepath.Join(backups, n)); err != nil {
			t.Errorf("pruning removed unrelated %s: %v", n, err)
		}
	}
}

func TestApplyMigrationsSnapshotFailureDoesNotBlock(t *testing.T) {
	buf := captureMigrateStderr(t)
	dir := t.TempDir()
	// A file where the backups folder should be: the snapshot can't be
	// written, migrating must go on anyway.
	if err := os.WriteFile(filepath.Join(dir, "backups"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, newest := openWithData(t, dir)
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations failed because of the snapshot: %v", err)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, newest).Scan(&n)
	if n != 1 {
		t.Errorf("v%d not applied", newest)
	}
	if !strings.Contains(buf.String(), "could not snapshot") {
		t.Errorf("no warning on stderr: %q", buf.String())
	}
}

func TestSchemaAheadDetected(t *testing.T) {
	buf := captureMigrateStderr(t)
	dir := t.TempDir()
	db, err := NewDatabase(filepath.Join(dir, "monoagent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dbV, binV, ahead, err := db.SchemaAhead(ctx)
	if err != nil || ahead || dbV != binV {
		t.Fatalf("up-to-date db: SchemaAhead = %d, %d, %v, %v", dbV, binV, ahead, err)
	}

	future := binV + 7
	if _, err := db.DB.Exec(`INSERT INTO schema_migrations (version, filename) VALUES (?, 'future_migration.sql')`, future); err != nil {
		t.Fatal(err)
	}
	dbV, binV, ahead, err = db.SchemaAhead(ctx)
	if err != nil || !ahead || dbV != future {
		t.Fatalf("ahead db: SchemaAhead = %d, %d, %v, %v", dbV, binV, ahead, err)
	}

	// Still runs, and says so once.
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations on a newer database must not refuse: %v", err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if c := strings.Count(out, "migrated by a newer monoagent"); c != 1 {
		t.Errorf("notice printed %d times, want 1: %q", c, out)
	}
	if !strings.Contains(out, "update monoagentcli") {
		t.Errorf("notice doesn't say to update: %q", out)
	}
}

func TestSchemaAheadWithoutMigrationsTable(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dbV, _, ahead, err := db.SchemaAhead(context.Background())
	if err != nil || ahead || dbV != 0 {
		t.Fatalf("SchemaAhead on empty db = %d, %v, %v", dbV, ahead, err)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master`).Scan(&n)
	if n != 0 {
		t.Errorf("SchemaAhead created %d schema objects; it must be read-only", n)
	}
}
