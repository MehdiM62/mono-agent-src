package aitask

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/storage"
)

// Queue creates a queued task for the ai.task workflow node and starts
// nothing: a person starts it from the bubble queue. It takes the launcher the
// commands use (this binary), over the database db has open.
func Queue(ctx context.Context, db *sql.DB, profileID string, in aitask.SubmitInput) (aitask.Task, error) {
	var path string
	if err := db.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil || path == "" {
		return aitask.Task{}, fmt.Errorf("locating the database: %v", err)
	}
	d := Deps{
		JSONOutput: func() bool { return true },
		ProfileID:  func() string { return profileID },
		DBPath:     func() string { return filepath.Clean(path) },
		OpenDB: func() (*storage.Database, error) {
			sdb, err := storage.NewDatabase(path)
			if err != nil {
				return nil, err
			}
			if err := sdb.ApplyMigrations(); err != nil {
				sdb.Close()
				return nil, err
			}
			return sdb, nil
		},
	}
	svc, closeDB, err := (&cli{d: &d}).open()
	if err != nil {
		return aitask.Task{}, err
	}
	defer closeDB()
	in.QueueOnly, in.Unattended = true, false
	return svc.Submit(ctx, in)
}
