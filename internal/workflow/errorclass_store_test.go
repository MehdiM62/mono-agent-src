package workflow

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// The class an execution failed with is written once (the first failure wins)
// unless forced, and reads back as "" when it is unset, when the execution is
// unknown, or when the column does not exist.
func TestExecutionErrorClassFirstFailureWinsUnlessForced(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE workflow_executions (id TEXT PRIMARY KEY, error_class TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO workflow_executions (id) VALUES ('e1')`); err != nil {
		t.Fatal(err)
	}

	if got := loadExecutionErrorClass(ctx, db, "e1"); got != "" {
		t.Fatalf("unset class = %q, want empty", got)
	}
	setExecutionErrorClass(ctx, db, "e1", ClassInvalidConfig, false)
	if got := loadExecutionErrorClass(ctx, db, "e1"); got != string(ClassInvalidConfig) {
		t.Fatalf("after first failure = %q, want %q", got, ClassInvalidConfig)
	}
	setExecutionErrorClass(ctx, db, "e1", ClassNotSetup, false)
	if got := loadExecutionErrorClass(ctx, db, "e1"); got != string(ClassInvalidConfig) {
		t.Fatalf("a later failure replaced the first: %q", got)
	}
	setExecutionErrorClass(ctx, db, "e1", ClassNotSetup, true)
	if got := loadExecutionErrorClass(ctx, db, "e1"); got != string(ClassNotSetup) {
		t.Fatalf("forced class = %q, want %q", got, ClassNotSetup)
	}
	if got := loadExecutionErrorClass(ctx, db, "missing"); got != "" {
		t.Fatalf("unknown execution = %q, want empty", got)
	}
	if got := loadExecutionErrorClass(ctx, nil, "e1"); got != "" {
		t.Fatalf("nil db = %q, want empty", got)
	}

	noCol, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { noCol.Close() })
	noCol.SetMaxOpenConns(1)
	if _, err := noCol.Exec(`CREATE TABLE workflow_executions (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if got := loadExecutionErrorClass(ctx, noCol, "e1"); got != "" {
		t.Fatalf("without the column = %q, want empty", got)
	}
}
