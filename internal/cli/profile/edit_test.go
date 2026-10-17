package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// runCaptured runs a profile command and returns what it printed to stdout.
func runCaptured(t *testing.T, dbPath string, args ...string) (out string, err error) {
	t.Helper()
	out = captureStdout(t, func() { _, err = runProfileCmd(t, dbPath, args...) })
	return out, err
}

func createTestProfile(t *testing.T, dbPath, name string) string {
	t.Helper()
	out, err := runCaptured(t, dbPath, "create", name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	var p profileJSON
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("create output %q: %v", out, err)
	}
	return p.ID
}

func TestProfileUpdateRenamesAndSetsIcon(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	id := createTestProfile(t, dbPath, "work")
	if _, err := runProfileCmd(t, dbPath, "update", id, "--name", "office", "--icon", "coder"); err != nil {
		t.Fatalf("update: %v", err)
	}
	out, err := runCaptured(t, dbPath, "get", id)
	if err != nil || !strings.Contains(out, `"name": "office"`) || !strings.Contains(out, `"icon": "coder"`) {
		t.Fatalf("get after update: %v %s", err, out)
	}
	createTestProfile(t, dbPath, "home")
	if _, err := runProfileCmd(t, dbPath, "update", id, "--name", "HOME"); err == nil {
		t.Fatal("renaming onto another profile's name should fail")
	}
	if _, err := runProfileCmd(t, dbPath, "update", id); err == nil {
		t.Fatal("update with no flags should fail")
	}
}

func TestProfileDeleteRemovesItsRowsOnly(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	id := createTestProfile(t, dbPath, "doomed")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{id, "default"} {
		if _, err := db.DB.Exec(`INSERT INTO tasks (profile_id, title, status, position, created_at, updated_at)
			VALUES (?, 'x', 'inbox', 1, '2026-10-05T12:00:00Z', '2026-10-05T12:00:00Z')`, p); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	if _, err := runProfileCmd(t, dbPath, "delete", id); err == nil {
		t.Fatal("delete without --yes should fail")
	}
	out, err := runCaptured(t, dbPath, "delete", id, "--summary")
	if err != nil || !strings.Contains(out, `"tasks"`) {
		t.Fatalf("summary: %v %s", err, out)
	}
	if _, err := runProfileCmd(t, dbPath, "delete", id, "--yes-delete-profile", "doomed"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	db, err = storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var gone, kept int
	db.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE profile_id = ?`, id).Scan(&gone)
	db.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE profile_id = 'default'`).Scan(&kept)
	if gone != 0 || kept != 1 {
		t.Fatalf("tasks left: deleted profile %d (want 0), default %d (want 1)", gone, kept)
	}
	if _, err := runProfileCmd(t, dbPath, "get", id); err == nil {
		t.Fatal("profile should be gone")
	}
}

func TestProfileDeleteMakesABackupAndKeepsTheFolder(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	id := createTestProfile(t, dbPath, "doomed")
	createTestProfile(t, dbPath, "other")
	folder := t.TempDir()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE profiles SET root_dir = ? WHERE id = ?`, folder, id); err != nil {
		t.Fatal(err)
	}
	db.Close()

	if _, err := runProfileCmd(t, dbPath, "delete", id, "--yes-delete-profile", "wrong"); err == nil {
		t.Fatal("a confirmation that is not the profile's name should fail")
	}
	out, err := runCaptured(t, dbPath, "delete", id, "--yes-delete-profile", "doomed")
	if err != nil {
		t.Fatal(err)
	}
	var res profileImpact
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if _, err := os.Stat(res.Backup); err != nil || !strings.Contains(res.Backup, "pre-delete-profile-") {
		t.Fatalf("backup %q: %v", res.Backup, err)
	}
	if _, err := os.Stat(folder); err != nil || !res.FolderKept {
		t.Fatalf("the profile's folder must be kept: %v", err)
	}
}

func TestProfileUpdateAndDeleteAreForTheOperatorOnly(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	id := createTestProfile(t, dbPath, "victim")
	createTestProfile(t, dbPath, "other")
	t.Setenv(workflow.EnvWorkflowRun, "exec-1")
	for _, args := range [][]string{
		{"update", id, "--name", "renamed"},
		{"delete", id, "--yes-delete-profile", "victim"},
	} {
		if _, err := runProfileCmd(t, dbPath, args...); err == nil || !strings.Contains(err.Error(), workflow.EnvWorkflowRun) {
			t.Errorf("%v from a workflow run: err %v, want a refusal naming %s", args, err, workflow.EnvWorkflowRun)
		}
	}
	// Reading what a delete would remove stays open.
	if _, err := runProfileCmd(t, dbPath, "delete", id, "--summary"); err != nil {
		t.Errorf("--summary from a workflow run: %v", err)
	}
	if _, err := runCaptured(t, dbPath, "get", id); err != nil {
		t.Fatalf("the profile must still exist: %v", err)
	}
}

// seedOverrides gives the columns whose CHECK constraint rejects the dummy value.
var seedOverrides = map[string]map[string]any{
	"applications":         {"kind": "job", "status": "pending"},
	"chat_memory_messages": {"role": "human"},
	"person_links":         {"relation": "same", "status": "confirmed", "person_a": 1, "person_b": 2},
}

// TestDeleteCoversEveryProfileScopedTable seeds one row for the profile in
// every table that has a profile_id column, deletes the profile, and fails if
// any row is left — so a table added later without a delete rule (or one this
// seeding cannot fill) is caught here.
func TestDeleteCoversEveryProfileScopedTable(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	id := createTestProfile(t, dbPath, "doomed")
	createTestProfile(t, dbPath, "other")

	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	rows, err := conn.QueryContext(ctx, `SELECT m.name FROM sqlite_master m WHERE m.type = 'table' AND m.name NOT LIKE 'sqlite_%'
		AND EXISTS (SELECT 1 FROM pragma_table_info(m.name) WHERE name = 'profile_id') ORDER BY m.name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	if len(tables) == 0 {
		t.Fatal("no profile-scoped tables found")
	}
	for _, tbl := range tables {
		cols, vals := []string{"profile_id"}, []any{id}
		var pkCols int // a lone INTEGER primary key is the rowid; part of a composite one needs a value
		conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE pk > 0`, tbl)).Scan(&pkCols)
		ci, err := conn.QueryContext(ctx, fmt.Sprintf(`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_info('%s')`, tbl))
		if err != nil {
			t.Fatal(err)
		}
		for ci.Next() {
			var name, typ string
			var notnull, pk int
			var dflt *string
			ci.Scan(&name, &typ, &notnull, &dflt, &pk)
			if name == "profile_id" || notnull == 0 || dflt != nil || (pk > 0 && pkCols == 1 && strings.EqualFold(typ, "INTEGER")) {
				continue
			}
			var v any = "x"
			switch strings.ToUpper(typ) {
			case "INTEGER", "INT", "BOOLEAN", "REAL", "NUMERIC":
				v = 1
			case "BLOB":
				v = []byte{0}
			}
			if o, ok := seedOverrides[tbl][name]; ok {
				v = o
			}
			cols, vals = append(cols, name), append(vals, v)
		}
		ci.Close()
		q := fmt.Sprintf(`INSERT INTO "%s" (%s) VALUES (%s)`, tbl, strings.Join(cols, ","), strings.TrimSuffix(strings.Repeat("?,", len(cols)), ","))
		if _, err := conn.ExecContext(ctx, q, vals...); err != nil {
			t.Errorf("seeding %s: %v (teach this test to fill it, so the delete is checked for it)", tbl, err)
		}
	}
	conn.Close()
	db.Close()

	if _, err := runProfileCmd(t, dbPath, "delete", id, "--yes-delete-profile", "doomed"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	db, err = storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tbl := range tables {
		var n int
		if err := db.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM "%s" WHERE profile_id = ?`, tbl), id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: %d row(s) of the deleted profile left behind", tbl, n)
		}
	}
}
