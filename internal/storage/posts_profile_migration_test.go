package storage

import (
	"path/filepath"
	"testing"
)

// Migration 097 scopes posts to a profile: ids and post_comments survive the
// rebuild, profile_id is backfilled from the owning person ('default' for
// ownerless posts), and two profiles may now hold the same shortcode.
func TestApplyMigration097_PostsProfileScoped(t *testing.T) {
	db, err := NewDatabase(filepath.Join(t.TempDir(), "posts.db"))
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.DB.Close()
	applyMigrationsBelow(t, db, 97)

	for _, stmt := range []string{
		`INSERT INTO people (id, platform_username, platform, profile_id) VALUES
			('pa', 'alice', 'INSTAGRAM', 'prof-a'), ('pb', 'bob', 'INSTAGRAM', 'prof-b')`,
		`INSERT INTO posts (id, person_id, platform, shortcode, url, scraped_at) VALUES
			('po-a', 'pa', 'INSTAGRAM', 'SC1', 'u1', 't'),
			('po-b', 'pb', 'INSTAGRAM', 'SC2', 'u2', 't'),
			('po-none', NULL, 'INSTAGRAM', 'SC3', 'u3', 't')`,
		`INSERT INTO post_comments (id, post_id, author, timestamp, scraped_at) VALUES
			('c1', 'po-a', 'x', 't1', 't'), ('c2', 'po-none', 'y', 't2', 't')`,
	} {
		if _, err := db.DB.Exec(stmt); err != nil {
			t.Fatalf("seeding %q: %v", stmt, err)
		}
	}

	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	want := map[string]string{"po-a": "prof-a", "po-b": "prof-b", "po-none": "default"}
	for id, profile := range want {
		var got string
		if err := db.DB.QueryRow(`SELECT profile_id FROM posts WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("post %s lost: %v", id, err)
		}
		if got != profile {
			t.Errorf("post %s profile_id = %q, want %q", id, got, profile)
		}
	}
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM post_comments c JOIN posts p ON p.id = c.post_id`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("post_comments joined to posts = %d (err %v), want 2", n, err)
	}

	ins := `INSERT INTO posts (id, platform, shortcode, url, scraped_at, profile_id) VALUES (?, 'INSTAGRAM', 'SC1', 'u', 't', ?)`
	if _, err := db.DB.Exec(ins, "po-a2", "prof-b"); err != nil {
		t.Fatalf("same shortcode in another profile must be allowed: %v", err)
	}
	if _, err := db.DB.Exec(ins, "po-a3", "prof-a"); err == nil {
		t.Fatal("same shortcode in the same profile must violate the unique key")
	}
}
