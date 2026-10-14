// wails-app/app_db_prepare.go
//
// The database belongs to the CLI. The app never applies schema migrations or
// the per-profile vault migrations itself: at startup it runs
// `monoagentcli --json db migrate` once, which opens the database, applies
// every pending migration and brings each profile's folder and vault files up
// to date. One binary migrates, so the app, the CLI and the daemon share one
// schema version. A CLI that refuses because nobody is signed in to monoes.me
// yet is asked again once `account status` reports a session.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// dbMigrateTimeout bounds `db migrate`. The first start after an update may
// apply many migrations and move vault files; a CLI that hangs past this must
// not keep the app from starting.
const dbMigrateTimeout = 5 * time.Minute

// dbMigrateResult is `monoagentcli --json db migrate`.
type dbMigrateResult struct {
	SchemaVersion int      `json:"schema_version"`
	Applied       int      `json:"applied"`
	ActiveProfile string   `json:"active_profile"`
	Profiles      []string `json:"profiles"`
	VaultMoved    int      `json:"vault_moved"`
	Errors        []string `json:"errors"`
}

// errDBMigrateNeedsLogin is the CLI gate's refusal (exit 4): no monoes.me
// session yet, so the database is migrated after sign-in instead.
var errDBMigrateNeedsLogin = errors.New("database migrations wait for the monoes.me sign-in")

// runDBMigrate runs `db migrate` and decodes its answer.
func runDBMigrate(parent context.Context) (*dbMigrateResult, error) {
	cliBin, err := findMonoAgentCLI()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, dbMigrateTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliBin, "--json", "db", "migrate")
	suppressConsole(cmd)
	out, runErr := cmd.Output()
	if accountExitCode(runErr) == 4 {
		return nil, errDBMigrateNeedsLogin
	}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) && cliStderr(ee.Stderr) != "" {
			return nil, fmt.Errorf("monoagentcli db migrate: %s", lastLine(cliStderr(ee.Stderr)))
		}
		return nil, fmt.Errorf("monoagentcli db migrate: %w", runErr)
	}
	var res dbMigrateResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &res); err != nil {
		return nil, fmt.Errorf("monoagentcli db migrate: unexpected output: %w", err)
	}
	return &res, nil
}

// prepareDatabase runs `db migrate` and reports its outcome in the app log.
// It reports whether the database is now migrated; on a refusal for sign-in
// it leaves dbMigratePending set so AccountStatus asks again.
func (a *App) prepareDatabase() bool {
	res, err := runDBMigrate(a.ctx)
	if errors.Is(err, errDBMigrateNeedsLogin) {
		a.dbMigratePending.Store(true)
		a.emitLog("SYSTEM", "INFO", errDBMigrateNeedsLogin.Error())
		return false
	}
	if err != nil {
		runtime.LogErrorf(a.ctx, "DB migration error: %v", err)
		a.emitLog("SYSTEM", "ERROR", fmt.Sprintf("DB migration error: %v", err))
		return false
	}
	for _, e := range res.Errors {
		a.emitLog("SYSTEM", "WARN", e)
	}
	if res.Applied > 0 {
		a.emitLog("SYSTEM", "INFO", fmt.Sprintf("applied %d database migration(s); schema version %d", res.Applied, res.SchemaVersion))
	}
	if res.VaultMoved > 0 {
		a.emitLog("SYSTEM", "INFO", fmt.Sprintf("moved %d vault file(s) into their profile folders", res.VaultMoved))
	}
	// Best-effort, fire-and-forget: seeds each profile's knowledge graph.
	for _, id := range res.Profiles {
		a.bootstrapProfileMonograph(id)
	}
	return true
}

// retryDBMigrateAfterLogin runs the migration a sign-in refusal postponed, once,
// when an account status document says the session now allows work.
func (a *App) retryDBMigrateAfterLogin(status string) {
	if !statusAllowsWork(status) || !a.dbMigratePending.CompareAndSwap(true, false) {
		return
	}
	go func() {
		if a.prepareDatabase() {
			// The watchers may have found no profile folder yet;
			// `db migrate` has now created it.
			a.startWatchers()
		}
	}()
}

// statusAllowsWork reports whether an AccountStatus answer is a session the
// CLI gate lets work through: ok or grace.
func statusAllowsWork(status string) bool {
	var st struct {
		State string `json:"state"`
	}
	return json.Unmarshal([]byte(status), &st) == nil && (st.State == "ok" || st.State == "grace")
}
