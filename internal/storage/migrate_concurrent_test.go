package storage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestMigrateRaceHelper is the child half of TestApplyMigrationsConcurrentProcesses:
// it opens the database named by the env var, waits for the start file so all
// children collide, and applies migrations.
func TestMigrateRaceHelper(t *testing.T) {
	dbPath := os.Getenv("MIGRATE_RACE_DB")
	if dbPath == "" {
		t.Skip("helper process only")
	}
	start := os.Getenv("MIGRATE_RACE_START")
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if _, err := os.Stat(start); err == nil {
			break
		}
	}
	db, err := NewDatabase(dbPath)
	if err != nil {
		t.Fatalf("NewDatabase: %v", err)
	}
	defer db.DB.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
}

// Several processes starting against the same unmigrated database (the
// desktop app launches many CLI calls at once) must all succeed and record
// every version exactly once.
func TestApplyMigrationsConcurrentProcesses(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "race.db")
	start := filepath.Join(dir, "go")

	const n = 6
	cmds := make([]*exec.Cmd, n)
	outs := make([]*os.File, n)
	for i := range cmds {
		out, err := os.Create(filepath.Join(dir, "out"+string(rune('a'+i))))
		if err != nil {
			t.Fatal(err)
		}
		outs[i] = out
		c := exec.Command(os.Args[0], "-test.run=^TestMigrateRaceHelper$", "-test.count=1")
		c.Env = append(os.Environ(), "MIGRATE_RACE_DB="+dbPath, "MIGRATE_RACE_START="+start)
		c.Stdout, c.Stderr = out, out
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = c
	}
	time.Sleep(300 * time.Millisecond)
	if err := os.WriteFile(start, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			b, _ := os.ReadFile(outs[i].Name())
			t.Errorf("process %d failed: %v\n%s", i, err, b)
		}
	}

	db, err := NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	var dup int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM (SELECT version FROM schema_migrations GROUP BY version HAVING COUNT(*) > 1)`).Scan(&dup); err != nil {
		t.Fatal(err)
	}
	if dup != 0 {
		t.Errorf("%d versions recorded more than once", dup)
	}
	// Already migrated: a further call takes the lock and finds nothing to do.
	if err := db.ApplyMigrations(); err != nil {
		t.Errorf("re-apply on a migrated database: %v", err)
	}
}
