package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/applyfill"
	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// applyScope is where this process may read and write files when it is an
// apply-kind agent (an application-apply task's turn), nil for any other
// caller. The task comes from the vouched kind token, never from an
// environment variable, and must be an open application-apply task of this
// profile; its folders are the snapshot and work folder next to the
// database. An apply caller whose task cannot be established is refused.
func applyScope(cfg *globalConfig) (*browserctl.ApplyScope, error) {
	if browserctl.AgentKind() != browserctl.KindApply {
		return nil, nil
	}
	id := browserctl.ApplyTaskID()
	if id == "" {
		return nil, errInvalidInput("this apply session carries no task, so it may not read or write files")
	}
	db, err := initDB(cfg)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	t, err := (&aitask.Store{DB: db.DB, Profile: cfg.ProfileID}).Get(id)
	if err != nil || !(t.Kind == applyfill.Kind || t.Kind == aitask.KindExtensionFill || t.Kind == aitask.KindExtensionAsk) || (t.Status != aitask.StatusRunning && t.Status != aitask.StatusNeedsUser) {
		return nil, errInvalidInput("this apply session's task %s is not an open application-apply or extension task of this profile", id)
	}
	snapshot := aitask.SnapshotDir(filepath.Dir(clicore.ExpandPath(cfg.DBPath)), id)
	return &browserctl.ApplyScope{ContextDir: snapshot, WorkDir: filepath.Join(filepath.Dir(snapshot), "work")}, nil
}

// scopeError is the CLI's refusal of a path outside the apply scope.
func scopeError(err error) error {
	return browserCLIError{error: errInvalidInput("%v", err), code: "path_not_allowed"}
}

// extensionTarget is the one tab an extension task's turn may act on: the
// tab, browser and origin recorded when the person queued it, read from the
// task's verified context snapshot. The task comes from the vouched kind
// token, like applyScope. nil for every other caller. An extension session
// whose task or record cannot be established is refused.
func extensionTarget(cfg *globalConfig, db *sql.DB) (*aitask.ExtensionTarget, error) {
	if browserctl.AgentKind() != browserctl.KindApply {
		return nil, nil
	}
	id := browserctl.ApplyTaskID()
	if id == "" {
		return nil, nil // applyScope refuses a task-less apply session where it matters
	}
	t, err := (&aitask.Store{DB: db, Profile: cfg.ProfileID}).Get(id)
	if err != nil || !aitask.IsExtensionKind(t.Kind) {
		return nil, nil
	}
	if t.Status != aitask.StatusRunning && t.Status != aitask.StatusNeedsUser {
		return nil, errInvalidInput("this session's task %s is not open", id)
	}
	snapshot := aitask.SnapshotDir(filepath.Dir(clicore.ExpandPath(cfg.DBPath)), id)
	raw, err := os.ReadFile(filepath.Join(snapshot, filepath.FromSlash(aitask.ExtensionTargetFile)))
	var tg aitask.ExtensionTarget
	if err != nil || json.Unmarshal(raw, &tg) != nil || tg.Tab <= 0 || tg.Origin == "" {
		return nil, errInvalidInput("task %s has no record of the tab it works on", id)
	}
	return &tg, nil
}
