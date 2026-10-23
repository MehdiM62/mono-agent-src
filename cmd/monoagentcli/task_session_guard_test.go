package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/storage"
)

const guardMsg = "an AI task's session may not do this"

// guardRig is a home with two profiles and a task of the given kind in p1.
func guardRig(t *testing.T, kind string) (taskID string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	asOperatorProcess(t)
	path := expandPath("~/.monoagent/monoagent.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := storage.NewDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.ApplyMigrations(); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`INSERT INTO profiles (id, name) VALUES ('p1','One')`, `INSERT INTO profiles (id, name) VALUES ('p2','Two')`} {
		if _, err := db.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	task, err := (&aitask.Store{DB: db.DB, Profile: "p1"}).Insert(aitask.Task{ID: aitask.NewID(), Kind: kind, ConversationID: "c", Status: aitask.StatusRunning, Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	return task.ID
}

func runGuardRoot(t *testing.T, args ...string) error {
	t.Helper()
	root := newRootCmd()
	root.SetArgs(args)
	var err error
	captureStdout(t, func() { err = root.Execute() })
	return err
}

// In a confined task's session the root hook refuses what would leave the
// task's profile, through the real command tree.
func TestTaskSessionRefusesFlagsThatLeaveTheTask(t *testing.T) {
	id := guardRig(t, "failed-run-fix")
	asTaskSession(t, id, "tok")
	for name, args := range map[string][]string{
		"other profile":      {"--profile", "p2", "workflow", "list"},
		"other profile name": {"--profile", "Two", "workflow", "list"},
		"profile twice":      {"--profile", "p1", "--profile", "p2", "workflow", "list"},
		"profile twice same": {"--profile", "p1", "--profile=p1", "workflow", "list"},
		"db-path":            {"--profile", "p1", "--db-path", "/tmp/x.db", "workflow", "list"},
		"config-dir":         {"--profile", "p1", "--config-dir", "/tmp/c", "workflow", "list"},
		"output-dir":         {"--profile", "p1", "--output-dir", "/tmp/o", "workflow", "list"},
		"log-file":           {"--profile", "p1", "--log-file", "/tmp/l", "workflow", "list"},
		"all-profiles":       {"--profile", "p1", "workflow", "list", "--all-profiles"},
		"validate --file":    {"--profile", "p1", "workflow", "validate", "--file", "/etc/passwd"},
	} {
		err := runGuardRoot(t, args...)
		if err == nil || !strings.Contains(err.Error(), guardMsg) {
			t.Errorf("%s: not refused by the task-session guard: %v", name, err)
		}
	}
	for name, args := range map[string][]string{
		"own profile id":   {"--profile", "p1", "workflow", "list"},
		"own profile name": {"--profile", "One", "workflow", "list"},
	} {
		if err := runGuardRoot(t, args...); err != nil && strings.Contains(err.Error(), guardMsg) {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
}

// Only an agent in a confined kind's session is held to it.
func TestTaskSessionGuardLeavesOperatorsAndGenericTasksAlone(t *testing.T) {
	id := guardRig(t, "failed-run-fix")
	asOperatorProcess(t)
	t.Setenv(aitask.EnvTaskID, id) // the supervisor carries the id but is the operator
	if err := runGuardRoot(t, "--profile", "p2", "workflow", "list"); err != nil && strings.Contains(err.Error(), guardMsg) {
		t.Errorf("the operator was refused: %v", err)
	}
	id = guardRig(t, "generic")
	asTaskSession(t, id, "tok")
	if err := runGuardRoot(t, "--profile", "p2", "workflow", "list"); err != nil && strings.Contains(err.Error(), guardMsg) {
		t.Errorf("a generic task was refused: %v", err)
	}
	// A task that cannot be found fails closed.
	guardRig(t, "draft-reply")
	asTaskSession(t, "at-0000000000000000", "tok")
	if err := runGuardRoot(t, "--profile", "p1", "workflow", "list"); err == nil || !strings.Contains(err.Error(), guardMsg) {
		t.Errorf("an unknown task id was let through: %v", err)
	}
}
