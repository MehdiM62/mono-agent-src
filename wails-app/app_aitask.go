// wails-app/app_aitask.go
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// The "Send to coder" buttons' bindings: each shells out to `monoagentcli
// aitask …` and imports no internal package, the CLI's JSON is the contract.

// selectionTextPrefix marks a context entry that is text, not a path: the
// button hands over the selected or error text, and this file writes it under
// the profile's folder and passes it as selection=<path>.
const selectionTextPrefix = "selection-text="

// aitaskInputDir is where that text lands, relative to the profile's folder.
const aitaskInputDir = ".monoagent/ai-task-input"

// maxAITaskInputBytes bounds one text a button can hand over.
const maxAITaskInputBytes = 256 << 10

// aitaskContextClasses are the classes a button may pass; the CLI refuses
// anything else, and message or people-thread content never goes in.
var aitaskContextClasses = map[string]bool{"posting": true, "facts": true, "document": true, "selection": true}

// AITask is a task as the CLI prints it. Fallback is "oneshot" (with Reason)
// when coder mode is off: nothing was created and the caller decides.
type AITask struct {
	TaskID         string `json:"taskId"`
	Kind           string `json:"kind"`
	ConversationID string `json:"conversationId"`
	Status         string `json:"status"`
	Goal           string `json:"goal"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	ApplicationID  string `json:"applicationId"`
	Error          string `json:"error"`
	Fallback       string `json:"fallback"`
	Reason         string `json:"reason"`
}

// AITaskState says whether a button can hand work to the coder, and why not.
type AITaskState struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

// aitaskRefusal is a --json failure: the CLI prints {"error","code",…} on stdout.
type aitaskRefusal struct {
	Error    string `json:"error"`
	Code     string `json:"code"`
	Fallback string `json:"fallback"`
}

// aitaskCLI runs `monoagentcli --profile P --json aitask <args>` into result.
// Unlike runMonoCLI it keeps the refusal JSON on stdout, whose code the
// caller needs.
func (a *App) aitaskCLI(result any, args ...string) error {
	full, err := a.scopedArgs(append([]string{"aitask"}, args...)...)
	if err != nil {
		return err
	}
	return a.jsonCLIInto(result, full)
}

func (a *App) jsonCLIInto(result any, full []string) error {
	bin, err := findMonoAgentCLI()
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, bin, full...)
	suppressConsole(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var ref aitaskRefusal
		_ = json.Unmarshal([]byte(lastLine(string(out))), &ref)
		msg := ref.Error
		if msg == "" {
			msg = cliStderr(stderr.Bytes())
		}
		if msg == "" {
			msg = err.Error()
		}
		return &aitaskError{msg: msg, code: ref.Code, fallback: ref.Fallback}
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(out, result)
}

type aitaskError struct{ msg, code, fallback string }

func (e *aitaskError) Error() string { return e.msg }

// SubmitAITask hands a goal to the coder. context entries are CLASS=PATH
// (CLASS in posting, facts, document, selection) or selection-text=<text>.
// With coder mode off it returns {Fallback:"oneshot", Reason} and no error.
func (a *App) SubmitAITask(kind, goal string, context []string, applicationID string) (AITask, error) {
	if err := a.signedOut(); err != nil {
		return AITask{}, err
	}
	args := []string{"submit", "--kind", kind, "--goal", goal}
	for _, entry := range context {
		arg, err := a.aitaskContextArg(entry)
		if err != nil {
			return AITask{}, err
		}
		args = append(args, "--context", arg)
	}
	if applicationID != "" {
		args = append(args, "--application", applicationID)
	}
	var t AITask
	if err := a.aitaskCLI(&t, args...); err != nil {
		var ae *aitaskError
		if errors.As(err, &ae) && ae.code == "coder_disabled" {
			return AITask{Fallback: "oneshot", Reason: ae.msg}, nil
		}
		return AITask{}, err
	}
	return t, nil
}

// aitaskContextArg turns one button entry into a CLASS=PATH argument.
func (a *App) aitaskContextArg(entry string) (string, error) {
	if text, ok := strings.CutPrefix(entry, selectionTextPrefix); ok {
		path, err := a.writeAITaskInput(text)
		if err != nil {
			return "", err
		}
		return "selection=" + path, nil
	}
	class, path, ok := strings.Cut(entry, "=")
	if !ok || path == "" || !aitaskContextClasses[class] {
		return "", fmt.Errorf("context %q must be CLASS=PATH with CLASS one of posting, facts, document, selection", class)
	}
	return entry, nil
}

// writeAITaskInput saves text (cut to the size cap) in a new 0600 file under
// the profile's folder and returns its path.
func (a *App) writeAITaskInput(text string) (string, error) {
	id, err := a.profileID()
	if err != nil {
		return "", err
	}
	root, err := a.profileFolder(id)
	if err != nil {
		return "", err
	}
	if len(text) > maxAITaskInputBytes {
		text = text[:maxAITaskInputBytes]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	dir := filepath.Join(root, filepath.FromSlash(aitaskInputDir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	path := filepath.Join(dir, hex.EncodeToString(b[:])+".txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// GetAITask is `aitask status <id>`.
func (a *App) GetAITask(id string) (AITask, error) {
	if err := a.signedOut(); err != nil {
		return AITask{}, err
	}
	var t AITask
	err := a.aitaskCLI(&t, "status", id)
	return t, err
}

// CancelAITask is `aitask cancel <id>`.
func (a *App) CancelAITask(id string) (AITask, error) {
	if err := a.signedOut(); err != nil {
		return AITask{}, err
	}
	var t AITask
	err := a.aitaskCLI(&t, "cancel", id)
	return t, err
}

// AITaskStatus says whether coder mode is on, from `coder status`, and the
// reason when a task cannot go to the coder. It never turns coder on.
func (a *App) AITaskStatus() (AITaskState, error) {
	if err := a.signedOut(); err != nil {
		return AITaskState{}, err
	}
	var st struct {
		Enabled bool  `json:"enabled"`
		Ready   *bool `json:"ready"`
	}
	full, err := a.scopedArgs("coder", "status")
	if err != nil {
		return AITaskState{}, err
	}
	if err := a.jsonCLIInto(&st, full); err != nil {
		return AITaskState{}, err
	}
	switch {
	case !st.Enabled:
		return AITaskState{Reason: "Coder mode is off. It runs a coding agent with access to a folder, so only you can turn it on, in Settings."}, nil
	case st.Ready != nil && !*st.Ready:
		return AITaskState{Reason: "Coder mode is on but not ready: the coder runtime needs an update. See Settings."}, nil
	}
	return AITaskState{Enabled: true}, nil
}
