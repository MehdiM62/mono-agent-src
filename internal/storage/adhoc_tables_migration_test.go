package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/connections"
)

// runtimeDDL runs every piece of schema the Go code creates at runtime,
// outside data/migrations/, against db: the AI store (chat messages,
// conversations, turns, events, answers), the connections store
// (connections, oauth_refresh_locks, platform_oauth_credentials), and
// ReconcileSchema (vault_keys).
func runtimeDDL(t *testing.T, d *Database) {
	t.Helper()
	ctx := context.Background()
	if _, err := ai.NewAIStore(d.DB); err != nil {
		t.Fatalf("ai.NewAIStore: %v", err)
	}
	st := connections.NewStore(d.DB)
	if err := st.EnsureTable(ctx); err != nil {
		t.Fatalf("connections EnsureTable: %v", err)
	}
	// An empty client secret needs no vault; this only runs the DDL.
	if err := st.SaveOAuthClient(ctx, "probe", "default", "client", ""); err != nil {
		t.Fatalf("SaveOAuthClient: %v", err)
	}
	if err := d.ReconcileSchema(ctx); err != nil {
		t.Fatalf("ReconcileSchema: %v", err)
	}
}

func userTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		out = append(out, n)
	}
	return out
}

// tableShape is PRAGMA table_info (in column order) plus the foreign keys.
func tableShape(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	var out []string
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("col %d %s %s notnull=%d default=%v pk=%d", cid, name, strings.ToUpper(ctype), notnull, dflt, pk))
	}
	rows.Close()
	rows, err = db.Query("PRAGMA foreign_key_list(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, seq int
		var ref, from, to, onUpd, onDel, match string
		if err := rows.Scan(&id, &seq, &ref, &from, &to, &onUpd, &onDel, &match); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("fk %s -> %s(%s) on delete %s", from, ref, to, onDel))
	}
	rows.Close()
	return out
}

// A database built by ApplyMigrations alone has every table the runtime
// DDL creates, with the same columns in the same order and the same keys.
func TestFreshMigrationsCoverRuntimeDDL(t *testing.T) {
	dir := t.TempDir()
	migrated, err := NewDatabase(filepath.Join(dir, "migrated.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	if err := migrated.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	runtime, err := NewDatabase(filepath.Join(dir, "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	runtimeDDL(t, runtime)

	have := map[string]bool{}
	for _, n := range userTables(t, migrated.DB) {
		have[n] = true
	}
	want := userTables(t, runtime.DB)
	for _, table := range []string{"ai_chat_messages", "ai_chat_answers", "ai_chat_conversations", "ai_chat_turns",
		"ai_chat_events", "connections", "oauth_refresh_locks", "platform_oauth_credentials", "vault_keys"} {
		found := false
		for _, n := range want {
			found = found || n == table
		}
		if !found {
			t.Errorf("runtime DDL no longer creates %s — update this test", table)
		}
	}
	for _, table := range want {
		if !have[table] {
			t.Errorf("table %s is created by runtime code but not by migrations", table)
			continue
		}
		got, exp := tableShape(t, migrated.DB, table), tableShape(t, runtime.DB, table)
		if !reflect.DeepEqual(got, exp) {
			t.Errorf("%s differs:\nmigrations: %v\nruntime:    %v", table, got, exp)
		}
	}
	if have["credentials"] {
		t.Error("legacy credentials table survived migration 096")
	}
}

// An existing database whose runtime code already created these tables
// (and holds rows in them) upgrades through 095/096 without any change to
// their shape or data.
func TestAdhocTablesMigrationIsNoOpOnExistingDatabase(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "existing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	applyMigrationsBelow(t, db, 95)
	runtimeDDL(t, db)

	seed := []string{
		`INSERT INTO connections (id, platform, method, label, created_at, updated_at, vault_ref) VALUES ('c1', 'github', 'oauth', 'me', 't', 't', 'v1')`,
		`INSERT INTO oauth_refresh_locks (connection_id, locked_at) VALUES ('c1', 't')`,
		`INSERT INTO ai_chat_messages (id, workflow_id, role, content, profile_id, created_at) VALUES ('m1', 'w', 'user', 'hi', 'default', 't')`,
		`INSERT INTO ai_chat_answers (profile_id, conversation_id, turn_id, agent_id, question_id, text, created_at) VALUES ('default', 'cv', 'tu', 'ag', 'q', 'yes', 't')`,
	}
	for _, s := range seed {
		if _, err := db.DB.Exec(s); err != nil {
			t.Fatalf("seeding %q: %v", s, err)
		}
	}

	tables := []string{"ai_chat_messages", "ai_chat_answers", "ai_chat_conversations", "connections", "oauth_refresh_locks", "platform_oauth_credentials"}
	before := map[string][]string{}
	for _, tb := range tables {
		before[tb] = tableShape(t, db.DB, tb)
	}

	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	for _, tb := range tables {
		if got := tableShape(t, db.DB, tb); !reflect.DeepEqual(got, before[tb]) {
			t.Errorf("%s changed:\nbefore: %v\nafter:  %v", tb, before[tb], got)
		}
	}
	for q, want := range map[string]int{
		`SELECT COUNT(*) FROM connections WHERE vault_ref = 'v1'`:       1,
		`SELECT COUNT(*) FROM oauth_refresh_locks`:                      1,
		`SELECT COUNT(*) FROM ai_chat_messages`:                         1,
		`SELECT COUNT(*) FROM ai_chat_answers WHERE text = 'yes'`:       1,
		`SELECT COUNT(*) FROM platform_oauth_credentials`:               1,
		`SELECT COUNT(*) FROM sqlite_master WHERE name = 'credentials'`: 0,
	} {
		var n int
		if err := db.DB.QueryRow(q).Scan(&n); err != nil || n != want {
			t.Errorf("%s = %d (%v), want %d", q, n, err, want)
		}
	}
}

// A connections / ai_chat_conversations table created before its later
// columns existed gets them from ReconcileSchema, without the runtime code.
func TestReconcileAddsLaterColumns(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	applyMigrationsBelow(t, db, 95) // 040 creates the base ai_chat_conversations
	if _, err := db.DB.Exec(`CREATE TABLE connections (
		id TEXT PRIMARY KEY, platform TEXT NOT NULL, method TEXT NOT NULL, label TEXT NOT NULL,
		account_id TEXT NOT NULL DEFAULT '', data TEXT NOT NULL DEFAULT '{}', status TEXT NOT NULL DEFAULT 'active',
		last_tested TEXT NOT NULL DEFAULT '', profile_id TEXT NOT NULL DEFAULT 'default',
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	for _, c := range addedColumns {
		ok, err := db.columnExists(context.Background(), c.table, c.column)
		if err != nil || !ok {
			t.Errorf("%s.%s missing after ApplyMigrations (%v)", c.table, c.column, err)
		}
	}
	// Idempotent.
	if err := db.ReconcileSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
}
