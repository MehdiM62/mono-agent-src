package workflow

import (
	"context"
	"database/sql"
)

// OnExecutionFinished, when set, is called once for every execution the
// engine finishes, after its final status (and error_class) is stored. It runs
// in its own goroutine with a detached context, so a slow or panicking hook
// never delays or breaks the run. nil (the default) does nothing. The CLI
// installs one at start-up (cmd/monoagentcli/run_memory.go), so the engine
// stays dependency-free.
var OnExecutionFinished func(ctx context.Context, db *sql.DB, executionID, status string)

// notifyExecutionFinished calls OnExecutionFinished for executionID.
func notifyExecutionFinished(db *sql.DB, executionID, status string) {
	h := OnExecutionFinished
	if h == nil || db == nil || executionID == "" {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		h(context.WithoutCancel(context.Background()), db, executionID, status)
	}()
}
