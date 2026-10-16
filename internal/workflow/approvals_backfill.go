package workflow

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"
)

// approvalsBackfilledKey is the settings row that says backfillApprovals ran.
const approvalsBackfilledKey = "workflow_approvals.backfilled"

// approvalsDir is where the CLI keeps workflow files (a variable so tests
// can point it elsewhere); "" reads the default, ~/.monoagent/workflows.
var approvalsDir = ""

// BackfillApprovals approves, once, the current definition of every workflow
// there is when approvals arrive: on the first open of the database after
// migration 106, before anything on that open can change a workflow (the
// CLI runs it after its migrations: storage.RegisterAfterMigrations). That
// migration approved only the definitions a version recorded, so a workflow
// never saved through the store or never run, or whose file was edited by
// hand, would otherwise have become an AI's. The definition is the one the
// engine runs: the workflow's file when it has one, else its SQLite rows. A
// workflow that can't be read is logged and left unapproved; a database
// failure leaves the step to the next open.
func BackfillApprovals(ctx context.Context, db *sql.DB) error {
	var done string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, approvalsBackfilledKey).Scan(&done); !errors.Is(err, sql.ErrNoRows) {
		return nil // done, or no settings table to keep the mark in
	}
	if err := approveCurrentDefinitions(ctx, db); err != nil {
		log.Printf("workflow approvals: the current definitions were not approved yet, trying again on the next open: %v", err)
		return nil
	}
	_, err := db.ExecContext(ctx, `INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)`,
		approvalsBackfilledKey, time.Now().UTC().Format(time.RFC3339))
	return err
}

func approveCurrentDefinitions(ctx context.Context, db *sql.DB) error {
	dir := approvalsDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, ".monoagent", "workflows")
	}
	var files *WorkflowFileStore
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		if files, err = NewWorkflowFileStore(dir); err != nil {
			return err
		}
	}
	sqlStore := NewSQLiteWorkflowStore(db)
	store := NewHybridWorkflowStore(files, sqlStore)
	rows, err := db.QueryContext(ctx, `SELECT id FROM workflows`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		wf, err := store.GetWorkflow(ctx, id)
		if err != nil {
			log.Printf("workflow approvals: workflow %s can't be read, so it is not approved (save or activate it to approve it): %v", id, err)
			continue
		}
		if wf == nil {
			continue
		}
		if err := sqlStore.approve(ctx, wf); err != nil {
			return err
		}
	}
	return nil
}
