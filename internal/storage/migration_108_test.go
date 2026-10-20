package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/data"
)

// Migration 108 (vault.save, #451) on databases that already have vault items.
// The databases are built here from the migrations before 108 (the schema of
// release v0.151.2, whose data/migrations end at 106) with synthetic rows:
// never from a copy of a real database.

// preVaultSaveRows are vault_images rows as the earlier ways into the vault
// write them: a workflow's image, an upload, a discovered file and a chat
// save, in two profiles, with and without labels and run ids.
var preVaultSaveRows = []map[string]any{
	{"id": "img-001", "seq": 1, "path": "/home/u/.monoagent/profiles/p-work/.monoagent/vault/img-001.png", "filename": "img-001.png",
		"size_bytes": 2048, "source": "gemini", "workflow_id": "wf-1", "execution_id": "ex-1", "label": "Logo", "profile_id": "p-work",
		"created_at": "2026-09-01 10:00:00"},
	{"id": "img-002", "seq": 2, "path": "/home/u/.monoagent/profiles/p-home/.monoagent/vault/img-002.jpg", "filename": "img-002.jpg",
		"size_bytes": 50000, "source": "upload", "workflow_id": nil, "execution_id": nil, "label": nil, "profile_id": "p-home",
		"created_at": "2026-09-02 11:00:00"},
	{"id": "img-003", "seq": 3, "path": "/home/u/.monoagent/profiles/p-work/images/scan.webp", "filename": "scan.webp",
		"size_bytes": 900, "source": "discovered", "workflow_id": nil, "execution_id": nil, "label": nil, "profile_id": "p-work",
		"created_at": "2026-09-03 12:00:00"},
	{"id": "img-004", "seq": 4, "path": "/home/u/.monoagent/profiles/p-home/images/chart.png", "filename": "chart.png",
		"size_bytes": 3333, "source": "chat", "workflow_id": nil, "execution_id": nil, "label": "Q3 chart", "profile_id": "p-home",
		"created_at": "2026-09-04 13:00:00"},
}

var preVaultSaveCols = []string{"id", "seq", "path", "filename", "size_bytes", "source", "workflow_id", "execution_id", "label", "profile_id", "created_at"}

// openPreVaultSave is a file-backed database at dir/monoagent.db with every
// migration before 108 applied and preVaultSaveRows planted.
func openPreVaultSave(t *testing.T, dir string) *Database {
	t.Helper()
	db, err := NewDatabase(filepath.Join(dir, "monoagent.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	applyMigrationsBelow(t, db, 108)
	for _, r := range preVaultSaveRows {
		args := make([]any, len(preVaultSaveCols))
		for i, c := range preVaultSaveCols {
			args[i] = r[c]
		}
		if _, err := db.DB.Exec(`INSERT INTO vault_images (`+strings.Join(preVaultSaveCols, ", ")+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, args...); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// vaultImagesRows reads every row's preVaultSaveCols, oldest first.
func vaultImagesRows(t *testing.T, db *sql.DB) [][]any {
	t.Helper()
	rows, err := db.Query(`SELECT ` + strings.Join(preVaultSaveCols, ", ") + ` FROM vault_images ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		vals := make([]any, len(preVaultSaveCols))
		ptrs := make([]any, len(vals))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, vals)
	}
	return out
}

func TestMigration108KeepsExistingVaultItems(t *testing.T) {
	captureMigrateStderr(t)
	dir := t.TempDir()
	db := openPreVaultSave(t, dir)
	before := vaultImagesRows(t, db.DB)

	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	if after := vaultImagesRows(t, db.DB); !reflect.DeepEqual(before, after) {
		t.Fatalf("existing rows changed:\nbefore %v\nafter  %v", before, after)
	}
	var withClient, withURL int
	db.DB.QueryRow(`SELECT COUNT(*) FROM vault_images WHERE client_id IS NOT NULL`).Scan(&withClient)
	db.DB.QueryRow(`SELECT COUNT(*) FROM vault_images WHERE source_url IS NOT NULL`).Scan(&withURL)
	if withClient != 0 || withURL != 0 {
		t.Errorf("existing rows got a client id (%d) or a source address (%d)", withClient, withURL)
	}

	// The index is partial and unique: the existing NULLs never collide, a
	// client id is unique within its profile only.
	var ddl string
	if err := db.DB.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_vault_images_client'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, "UNIQUE") || !strings.Contains(ddl, "WHERE client_id IS NOT NULL") {
		t.Errorf("index = %s", ddl)
	}
	insert := func(id string, seq int, profile string, client any) error {
		_, err := db.DB.Exec(`INSERT INTO vault_images (id, seq, path, filename, size_bytes, source, profile_id, client_id)
			VALUES (?, ?, '/v/'||?, ?, 1, 'extension', ?, ?)`, id, seq, id, id, profile, client)
		return err
	}
	if err := insert("img-005", 5, "p-work", nil); err != nil {
		t.Errorf("another row without a client id: %v", err)
	}
	if err := insert("img-006", 6, "p-work", "v-1"); err != nil {
		t.Fatalf("a client id: %v", err)
	}
	if err := insert("img-007", 7, "p-work", "v-1"); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("the same client id twice in one profile: %v", err)
	}
	if err := insert("img-008", 8, "p-home", "v-1"); err != nil {
		t.Errorf("the same client id in another profile: %v", err)
	}

	// Running again is a no-op: 108 is recorded once.
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("second ApplyMigrations: %v", err)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 108`).Scan(&n)
	if n != 1 {
		t.Errorf("108 recorded %d times", n)
	}
}

// Startup migrations are covered by the pre-migration snapshot: the copy taken
// before 108 runs holds the vault items and the schema without 108's columns.
func TestMigration108IsCoveredByThePreMigrationSnapshot(t *testing.T) {
	captureMigrateStderr(t)
	dir := t.TempDir()
	db := openPreVaultSave(t, dir)
	before := vaultImagesRows(t, db.DB)
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	snaps := listSnapshots(t, dir)
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %v, want one", snaps)
	}
	snap, err := NewDatabase(filepath.Join(dir, "backups", snaps[0]))
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	if got := vaultImagesRows(t, snap.DB); !reflect.DeepEqual(got, before) {
		t.Errorf("snapshot rows = %v, want %v", got, before)
	}
	for _, col := range []string{"client_id", "source_url"} {
		if has, err := snap.columnExists(context.Background(), "vault_images", col); err != nil || has {
			t.Errorf("snapshot has %s (%v): it was taken after 108", col, err)
		}
	}
}

// 108 runs as one transaction: undone halfway (a killed run), it leaves
// neither column, nor the index, nor its record, and the next run applies it
// whole. SQLite's DDL is transactional; the runner records each migration in
// the transaction that applies it (applyMigrations).
func TestMigration108KilledHalfwayIsSafeToRerun(t *testing.T) {
	captureMigrateStderr(t)
	db := openPreVaultSave(t, t.TempDir())
	content, err := data.MigrationsFS.ReadFile("migrations/108_vault_images_client_id.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmts := splitStatements(string(content))
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(stmts[0]); err != nil { // the first ALTER, then the "kill"
		t.Fatal(err)
	}
	var mid int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('vault_images') WHERE name = 'client_id'`).Scan(&mid); err != nil || mid != 1 {
		t.Fatalf("the first statement did not add client_id inside the transaction (%d, %v)", mid, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if has, _ := db.columnExists(ctx, "vault_images", "client_id"); has {
		t.Fatal("a rolled-back 108 left client_id behind")
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("re-run after the kill: %v", err)
	}
	for _, col := range []string{"client_id", "source_url"} {
		if has, err := db.columnExists(ctx, "vault_images", col); err != nil || !has {
			t.Errorf("after the re-run %s is missing (%v)", col, err)
		}
	}
}
