package storage

import (
	"path/filepath"
	"testing"
)

// TestApplyMigration115_ApplicationAssets_BackfillAndChildren upgrades a
// database that sits just below 115 and holds a profile's application with
// tags and a status history, a document owned by it, and a document owned by
// an application id that does not exist. The rebuild of applications must keep
// the children, link the valid document with the right profile, and skip the
// dangling one without failing the migration.
func TestApplyMigration115_ApplicationAssets_BackfillAndChildren(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "upgrade-115.db"))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.DB.Close()

	applyMigrationsBelow(t, db, 115)
	exec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := db.DB.Exec(query, args...); err != nil {
			t.Fatalf("seeding %q: %v", query, err)
		}
	}
	exec(`INSERT INTO applications (id, profile_id, kind, status, created_at, updated_at) VALUES ('app-1', 'work', 'job', 'pending', 't', 't')`)
	exec(`INSERT INTO application_tags (application_id, tag) VALUES ('app-1', 'urgent')`)
	exec(`INSERT INTO application_status_log (id, application_id, from_status, to_status, actor, note, created_at) VALUES ('l1', 'app-1', '', 'pending', 'system', '', 't')`)
	exec(`INSERT INTO vault_documents (id, seq, path, filename, size_bytes, source, application_id, profile_id, created_at) VALUES ('doc-001', 1, '/v/a.pdf', 'a.pdf', 1, 'upload', 'app-1', 'work', '2026-10-01 10:00:00')`)
	exec(`INSERT INTO vault_documents (id, seq, path, filename, size_bytes, source, application_id, profile_id, created_at) VALUES ('doc-002', 2, '/v/b.pdf', 'b.pdf', 1, 'upload', 'gone', 'work', '2026-10-01 10:00:00')`)
	exec(`INSERT INTO vault_documents (id, seq, path, filename, size_bytes, source, application_id, profile_id, created_at) VALUES ('doc-003', 3, '/v/c.pdf', 'c.pdf', 1, 'upload', 'app-1', 'other', '2026-10-01 10:00:00')`)
	exec(`INSERT INTO vault_documents (id, seq, path, filename, size_bytes, source, profile_id, created_at) VALUES ('doc-004', 4, '/v/d.pdf', 'd.pdf', 1, 'upload', 'work', '2026-10-01 10:00:00')`)

	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations over a database with a dangling document: %v", err)
	}

	count := func(query string, args ...interface{}) int {
		t.Helper()
		var n int
		if err := db.DB.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatalf("%q: %v", query, err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM application_links`); n != 1 {
		t.Fatalf("application_links has %d rows, want only the valid document", n)
	}
	var profile, source string
	if err := db.DB.QueryRow(`SELECT profile_id, source FROM application_links WHERE application_id = 'app-1' AND kind = 'document' AND ref_id = 'doc-001'`).Scan(&profile, &source); err != nil {
		t.Fatalf("valid document not linked: %v", err)
	}
	if profile != "work" || source != "backfill" {
		t.Fatalf("link profile/source = %q/%q, want work/backfill", profile, source)
	}
	if n := count(`SELECT COUNT(*) FROM application_tags WHERE application_id = 'app-1'`); n != 1 {
		t.Fatalf("tags after rebuild = %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM application_status_log WHERE application_id = 'app-1'`); n != 1 {
		t.Fatalf("status log after rebuild = %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM applications WHERE id = 'app-1' AND profile_id = 'work' AND status = 'pending'`); n != 1 {
		t.Fatalf("application row not carried over")
	}
	// The wider status check is in force after the rebuild.
	if _, err := db.DB.Exec(`UPDATE applications SET status = 'needs_you' WHERE id = 'app-1'`); err != nil {
		t.Fatalf("needs_you refused after rebuild: %v", err)
	}
	// And the vault column that the backfill read from is untouched.
	if n := count(`SELECT COUNT(*) FROM vault_documents WHERE application_id IS NOT NULL`); n != 3 {
		t.Fatalf("vault_documents.application_id rows = %d, want 3", n)
	}
}
