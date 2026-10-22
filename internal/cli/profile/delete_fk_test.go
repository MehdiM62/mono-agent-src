package profile

import (
	"context"
	"fmt"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

// looseReferences are the foreign keys to a profile's tables that neither cascade nor
// clear themselves. deleteProfile repairs them (repairDanglingReferences); a key not
// listed here is a new way for a delete to fail, so it must cascade, SET NULL, or be
// added here once the delete is shown to cope with it.
var looseReferences = map[string]string{
	"posts.person_id":                 "a post of another profile can name one of this profile's people",
	"workflow_node_targets.person_id": "a target with no execution (or of another profile's) can name one of this profile's people",
}

// TestEveryForeignKeyToAProfileTableCascadesOrIsRepaired fails for a table added later
// whose foreign key into what a profile owns would make deleting the profile fail.
func TestEveryForeignKeyToAProfileTableCascadesOrIsRepaired(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	scope, err := profileScope(ctx, db.DB)
	if err != nil {
		t.Fatal(err)
	}
	owned := map[string]bool{"profiles": true}
	for _, e := range scope {
		owned[e.Table] = true
	}
	tables, _ := queryStrings(ctx, db.DB, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	seen := map[string]bool{}
	for _, tbl := range tables {
		rows, err := db.DB.Query(fmt.Sprintf(`SELECT "table", "from", on_delete FROM pragma_foreign_key_list('%s')`, tbl))
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var parent, from, onDelete string
			if err := rows.Scan(&parent, &from, &onDelete); err != nil {
				t.Fatal(err)
			}
			if !owned[parent] || onDelete == "CASCADE" || onDelete == "SET NULL" {
				continue
			}
			key := tbl + "." + from
			seen[key] = true
			if looseReferences[key] == "" {
				t.Errorf("%s references %s (ON DELETE %s) and nothing makes a profile delete cope with it: cascade it, or list it in looseReferences", key, parent, onDelete)
			}
		}
		rows.Close()
	}
	for key := range looseReferences {
		if !seen[key] {
			t.Errorf("looseReferences lists %s, which is no longer such a key: drop it", key)
		}
	}
}

// A profile can be deleted while rows of other profiles (or of none) still point at its
// people through foreign keys that do not cascade: it must not fail with FOREIGN KEY
// constraint failed (787), and it must leave no dangling reference.
func TestDeleteProfileWithRowsPointingAtItsPeople(t *testing.T) {
	dbPath := newProfileDocsCLITestDB(t)
	id := createTestProfile(t, dbPath, "doomed")
	other := createTestProfile(t, dbPath, "other")
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, q := range []string{
		fmt.Sprintf(`INSERT INTO people (id, platform_username, platform, profile_id) VALUES ('p1', 'u', 'INSTAGRAM', '%s')`, id),
		fmt.Sprintf(`INSERT INTO posts (id, person_id, platform, shortcode, url, scraped_at, profile_id) VALUES ('post1', 'p1', 'INSTAGRAM', 'sc', 'u', 'now', '%s')`, other),
		`INSERT INTO workflow_node_targets (id, execution_id, node_id, person_id, platform) VALUES ('t1', NULL, 'n', 'p1', 'INSTAGRAM')`,
		fmt.Sprintf(`INSERT INTO agent_tokens (id, profile_id, name, scope, prefix, token_hash, created_at) VALUES ('tok', '%s', 'n', 's', 'p', 'h', 'now')`, id),
		fmt.Sprintf(`INSERT INTO vault_key_hints (profile_id, bound_path) VALUES ('%s', 'h')`, id),
	} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := deleteProfile(ctx, db.DB, id); err != nil {
		t.Fatalf("deleteProfile: %v", err)
	}
	defer db.Close()
	if left, err := foreignKeyViolations(ctx, db.DB); err != nil || len(left) != 0 {
		t.Fatalf("dangling references after the delete: %v (err %v)", left, err)
	}
	var person, target *string
	if err := db.DB.QueryRow(`SELECT person_id FROM posts WHERE id = 'post1'`).Scan(&person); err != nil || person != nil {
		t.Errorf("the other profile's post should stay with no person: %v %v", person, err)
	}
	if err := db.DB.QueryRow(`SELECT person_id FROM workflow_node_targets WHERE id = 't1'`).Scan(&target); err != nil || target != nil {
		t.Errorf("the node target should stay with no person: %v %v", target, err)
	}
	for _, tbl := range []string{"people", "agent_tokens", "vault_key_hints"} {
		var n int
		db.DB.QueryRow(`SELECT COUNT(*) FROM ` + tbl).Scan(&n)
		if n != 0 {
			t.Errorf("%s: %d row(s) of the deleted profile left", tbl, n)
		}
	}
}
