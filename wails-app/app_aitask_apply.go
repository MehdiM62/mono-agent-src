package main

import (
	"encoding/json"
	"errors"
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
		// The CLI's typed refusal (stale_proposal, already_applied,
		// operator_only, …) leads the message as "code: message".
		var ae *aitaskError
		if errors.As(err, &ae) && ae.code != "" {
			return "", fmt.Errorf("%s: %s", ae.code, ae.msg)
		}
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
// allowActive adds --allow-active, which the GUI sets only after the user
// confirmed that the workflow is running or scheduled. These bindings exist
// only in the Wails app: the HTTP agent API and the extension bridge never
// expose them.
func (a *App) ApplyFailedRunFix(taskID string, allowActive bool) (string, error) {
	if allowActive {
		return a.aitaskApplyCLI("apply-fix", taskID, "--allow-active")
	}
	return a.aitaskApplyCLI("apply-fix", taskID)
}

// SaveGeneratedWorkflow is `workflow save-draft`: it saves a workflow-generate
// task's workflow as an inactive draft.
func (a *App) SaveGeneratedWorkflow(taskID string) (string, error) {
	return a.aitaskApplyCLI("save-draft", taskID)
}
