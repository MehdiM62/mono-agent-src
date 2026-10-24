package main

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// The propose-then-apply bindings: a confined task only proposes (a fix to a
// workflow, a generated workflow); the user reviews it in the task header and
// these three calls, made from the GUI window as the operator, apply it
// through the CLI's validated commands. The CLI's JSON is the contract.

// aitaskIDPattern is the shape of a task id: letters, digits, '-' and '_',
// never a leading '-'. Anything else is refused before the CLI is run.
var aitaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// aitaskApplyCLI runs `workflow <verb> [flags] -- <taskID>` and returns the
// CLI's JSON. The id comes after `--`, so it can never be read as a flag.
func (a *App) aitaskApplyCLI(verb, taskID string, flags ...string) (string, error) {
	if err := a.signedOut(); err != nil {
		return "", err
	}
	if !aitaskIDPattern.MatchString(taskID) {
		return "", fmt.Errorf("invalid task id")
	}
	args := append([]string{"workflow", verb}, flags...)
	full, err := a.scopedArgs(append(args, "--", taskID)...)
	if err != nil {
		return "", err
	}
	var out json.RawMessage
	if err := a.jsonCLIInto(&out, full); err != nil {
		return "", err
	}
	return string(out), nil
}

// PreviewFailedRunFix is `workflow apply-fix --dry-run`: the edits a
// failed-run-fix task proposes, as before/after, and warnings. It changes
// nothing.
func (a *App) PreviewFailedRunFix(taskID string) (string, error) {
	return a.aitaskApplyCLI("apply-fix", taskID, "--dry-run")
}

// ApplyFailedRunFix is `workflow apply-fix`: it applies the proposed edits to
// the workflow, which then needs the user's approval again before it runs.
func (a *App) ApplyFailedRunFix(taskID string) (string, error) {
	return a.aitaskApplyCLI("apply-fix", taskID)
}

// SaveGeneratedWorkflow is `workflow save-draft`: it saves a workflow-generate
// task's workflow as an inactive draft.
func (a *App) SaveGeneratedWorkflow(taskID string) (string, error) {
	return a.aitaskApplyCLI("save-draft", taskID)
}
