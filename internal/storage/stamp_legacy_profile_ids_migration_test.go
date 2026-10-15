package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// stampedTables are the tables migration 099 stamps.
var stampedTables = []string{
	"agent_usage", "ai_chat_messages", "applications", "chat_memory_messages",
	"connections", "crawler_sessions", "data_tables", "evaluation_runs",
	"hil_pending", "jev_usage", "people", "person_links", "person_messages",
	"person_status_updates", "platform_oauth_credentials", "publications",
	"send_allow", "social_lists", "tags", "threads", "vault_documents",
	"vault_images", "vault_secrets", "workflow_executions",
	"workflow_versions", "workflows",
}

// seedCheckValues satisfies the CHECK constraints on NOT NULL text columns.
var seedCheckValues = map[string]string{
	"applications.kind":         "job",
	"applications.status":       "pending",
	"chat_memory_messages.role": "human",
	"person_links.relation":     "same",
	"person_links.status":       "confirmed",
}

// seedRow inserts one row into table with the given profile_id (nil = NULL),
// filling every other NOT NULL column that has no default with a value
// distinct per call.
func seedRow(t *testing.T, db *Database, table string, profile any, n int) {
	t.Helper()
	rows, err := db.DB.Query(`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("table_info %s: %v", table, err)
	}
	var cols []string
	var args []any
	for rows.Next() {
		var name, typ string
		var notnull, pk int
		var dflt *string
		if err := rows.Scan(&name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		switch {
		case name == "profile_id":
			cols = append(cols, name)
			args = append(args, profile)
		case notnull == 1 && dflt == nil:
			cols = append(cols, name)
			if v, ok := seedCheckValues[table+"."+name]; ok {
				args = append(args, v)
				continue
			}
			switch strings.ToUpper(typ) {
			case "INTEGER", "INT", "REAL", "NUMERIC":
				args = append(args, n)
			case "BLOB":
				args = append(args, []byte{byte(n)})
			default:
				args = append(args, fmt.Sprintf("%s-%d", name, n))
			}
		}
	}
	rows.Close()
	ph := strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",")
	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`, table, strings.Join(cols, ","), ph)
	if _, err := seedConns[db].ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

func profileOf(t *testing.T, db *Database, table string, n int) string {
	t.Helper()
	// rows are told apart by rowid order: row 1 is ownerless, row 2 is owned.
	var got *string
	q := fmt.Sprintf(`SELECT profile_id FROM %s ORDER BY rowid LIMIT 1 OFFSET %d`, table, n)
	if err := db.DB.QueryRow(q).Scan(&got); err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	if got == nil {
		return "<NULL>"
	}
	return *got
}

// seedConns pins one foreign-key-free connection per test database for seeding.
var seedConns = map[*Database]*sql.Conn{}

func openBelow099(t *testing.T) *Database {
	t.Helper()
	db, err := NewDatabase(filepath.Join(t.TempDir(), "stamp.db"))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	t.Cleanup(func() { db.DB.Close() })
	applyMigrationsBelow(t, db, 99)
	conn, err := db.DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	seedConns[db] = conn
	t.Cleanup(func() { conn.Close(); delete(seedConns, db) })
	return db
}

func TestApplyMigration099_StampsOwnerlessRowsOnEveryTable(t *testing.T) {
	db := openBelow099(t)
	for _, stmt := range []string{
		`DELETE FROM profiles`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('newer', 'N', '2026-05-01 00:00:00'), ('oldest', 'O', '2026-01-01 00:00:00')`,
	} {
		if _, err := db.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for _, tbl := range stampedTables {
		seedRow(t, db, tbl, "", 1)
		seedRow(t, db, tbl, "newer", 2)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	for _, tbl := range stampedTables {
		if got := profileOf(t, db, tbl, 0); got != "oldest" {
			t.Errorf("%s: ownerless row stamped %q, want the oldest profile", tbl, got)
		}
		if got := profileOf(t, db, tbl, 1); got != "newer" {
			t.Errorf("%s: owned row changed to %q, want it untouched", tbl, got)
		}
	}
}

func TestApplyMigration099_PrefersTheDefaultProfile(t *testing.T) {
	db := openBelow099(t)
	for _, stmt := range []string{
		`DELETE FROM profiles`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('older', 'O', '2020-01-01 00:00:00'), ('default', 'D', '2026-05-01 00:00:00')`,
	} {
		if _, err := db.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	seedRow(t, db, "people", "", 1)
	seedRow(t, db, "workflows", "", 1)
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"people", "workflows"} {
		if got := profileOf(t, db, tbl, 0); got != "default" {
			t.Errorf("%s stamped %q, want default (it exists)", tbl, got)
		}
	}
}

func TestApplyMigration099_NoProfilesIsANoOp(t *testing.T) {
	db := openBelow099(t)
	if _, err := db.DB.Exec(`DELETE FROM profiles`); err != nil {
		t.Fatal(err)
	}
	seedRow(t, db, "people", "", 1)
	seedRow(t, db, "workflows", "", 1)
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("a database with no profiles must still migrate: %v", err)
	}
	for _, tbl := range []string{"people", "workflows"} {
		if got := profileOf(t, db, tbl, 0); got != "" {
			t.Errorf("%s profile_id = %q, want it left empty (no owner to give)", tbl, got)
		}
	}
}

func TestApplyMigration099_IdempotentAndDropsTheExpressionIndex(t *testing.T) {
	db := openBelow099(t)
	seedRow(t, db, "people", "", 1)
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	first := profileOf(t, db, "people", 0)
	// Re-run the migration body by hand: nothing may move.
	if _, err := db.DB.Exec(`DELETE FROM schema_migrations WHERE version = 99`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE profiles SET created_at = '1999-01-01 00:00:00' WHERE id <> 'default'`); err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := profileOf(t, db, "people", 0); got != first {
		t.Errorf("second run moved the row: %q -> %q", first, got)
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_workflows_profile_default'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("expression index still present (n=%d err=%v)", n, err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_workflows_profile'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("plain idx_workflows_profile missing (n=%d err=%v)", n, err)
	}
}

// vault_keys is the one nullable profile_id; ReconcileSchema (not the SQL
// file, which a drifted database could abort) stamps it.
func TestReconcileStampsAnOwnerlessVaultKey(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "vk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`DELETE FROM profiles`,
		`INSERT INTO profiles (id, name, created_at) VALUES ('newer', 'N', '2026-05-01 00:00:00'), ('oldest', 'O', '2026-01-01 00:00:00')`,
		`INSERT INTO vault_keys (profile_id, wrapped_dek, wrapped_nonce, created_at) VALUES (NULL, x'01', x'02', 't'), ('newer', x'03', x'04', 't')`,
	} {
		if _, err := db.DB.Exec(stmt); err != nil {
			t.Fatalf("%q: %v", stmt, err)
		}
	}
	for i := 0; i < 2; i++ { // second run: idempotent
		if err := db.ReconcileSchema(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM vault_keys WHERE profile_id = 'oldest'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("ownerless key stamped to oldest: n=%d err=%v", n, err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM vault_keys WHERE profile_id = 'newer'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("owned key must stay: n=%d err=%v", n, err)
	}
}
