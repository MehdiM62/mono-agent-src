package health

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/monoes/mono-agent/internal/statedir"
)

// CheckPerms reports a data folder or database other users can read.
const CheckPerms = "core.perms"

// FixPerms makes the data folder, its files and the database private.
const FixPerms = "core.perms.repair"

func permsCheck() Check {
	return Check{ID: CheckPerms, Group: GroupCore, Title: "Data folder privacy", DependsOn: []string{CheckHome}, Run: checkPerms}
}

func permsFix() Fix {
	return Fix{FixInfo: FixInfo{ID: FixPerms, Label: "Make data folder private", Safety: SafetyAuto}, Apply: fixPerms}
}

func checkPerms(_ context.Context, env *Env) Result {
	if runtime.GOOS == "windows" {
		return Result{Status: StatusSkip, Summary: "not applicable on Windows"}
	}
	found, err := statedir.FindExposed(env.DataDir, statedir.DBFiles(env.DBPath)...)
	if err != nil {
		return Result{Status: StatusWarn, Summary: "cannot read permissions of " + env.DataDir, Detail: err.Error()}
	}
	if len(found) == 0 {
		return Result{Status: StatusOK, Summary: "only you can read " + env.DataDir}
	}
	lines := make([]string, len(found))
	for i, e := range found {
		lines[i] = fmt.Sprintf("%v %s", e.Mode, e.Path)
	}
	return Result{
		Status:  StatusWarn,
		Summary: fmt.Sprintf("other users on this machine can read %d item(s) holding your people, messages and chats", len(found)),
		Detail:  strings.Join(lines, "\n"),
		FixID:   FixPerms,
	}
}

func fixPerms(_ context.Context, env *Env, progress func(string)) error {
	fixed, err := statedir.Repair(env.DataDir, statedir.DBFiles(env.DBPath)...)
	for _, e := range fixed {
		progress("made private: " + e.Path)
	}
	return err
}
