package aitask

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

// Env names the variables a task's turn runs with: the task and the token that
// lets its holder, and no one else, complete or fail it.
const (
	EnvTaskID = "MONOAGENT_AITASK_ID"
	EnvToken  = "MONOAGENT_AITASK_TOKEN"
)

// Caps on what a task carries.
const (
	MaxGoalBytes   = 16 << 10
	MaxResultBytes = 4 << 20
	maxErrorBytes  = 2000
)

// Conversation is the coder conversation a task runs in.
type Conversation struct {
	ID      string
	Cwd     string
	Runtime string
}

// Launcher is what a task needs from the rest of the CLI: a coder
// conversation and a detached process that runs its first turn. The CLI
// implements it over `chat history create --mode coder` and `chat --turn`
// (the code coder queue start uses); tests stand in for it.
type Launcher interface {
	// Ready is nil when a coder conversation can be created, else the refusal
	// (an *Error with CodeCoderDisabled when coder mode is off).
	Ready(ctx context.Context) error
	// Policy is a fingerprint of the settings a task runs under (the coder
	// settings): `start` refuses a task whose policy has changed since submit.
	Policy(ctx context.Context) (string, error)
	CreateConversation(ctx context.Context, runtime string) (Conversation, error)
	// Detach starts the process that runs task id's turn, with EnvTaskID and
	// EnvToken in its environment, and returns without waiting for it.
	Detach(ctx context.Context, taskID, token string) error
}

// Service is the task operations over a store and a launcher.
type Service struct {
	Store    *Store
	Launcher Launcher
	// Redact hides secret values in the context and the text an agent hands
	// back; nil leaves text as it is.
	Redact    func(string) string
	PollEvery time.Duration
}

// SubmitInput is `aitask submit`.
type SubmitInput struct {
	Kind          string
	Goal          string
	Context       []string
	ApplicationID string
	Access        string
	Runtime       string
	Unattended    bool
	QueueOnly     bool
}

// Submit creates the task and its coder conversation, writes its context and
// starts turn 1 (unless QueueOnly). It never falls back to anything: when
// coder mode is off it says so with CodeCoderDisabled, and the caller decides.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (Task, error) {
	goal := strings.TrimSpace(in.Goal)
	switch {
	case goal == "":
		return Task{}, errf(CodeInvalidInput, "--goal is required")
	case len(goal) > MaxGoalBytes || !utf8.ValidString(goal):
		return Task{}, errf(CodeInvalidInput, "--goal must be UTF-8 text of at most %d KB", MaxGoalBytes>>10)
	case strings.ContainsAny(in.ApplicationID, "\x00\r\n") || len(in.ApplicationID) > 200:
		return Task{}, errf(CodeInvalidInput, "invalid --application")
	case !ValidAccess(in.Access):
		return Task{}, errf(CodeInvalidInput, "--access must be coding, workspace or qa")
	}
	kind, err := Lookup(in.Kind)
	if err != nil {
		return Task{}, err
	}
	access := in.Access
	if access == "" {
		access = kind.DefaultAccess
	}
	if access == "" {
		access = AccessCoding
	}
	if err := checkUnattended(kind, access, in.Unattended); err != nil {
		return Task{}, err
	}
	if err := s.Launcher.Ready(ctx); err != nil {
		return Task{}, err
	}
	conv, err := s.Launcher.CreateConversation(ctx, in.Runtime)
	if err != nil {
		return Task{}, err
	}
	t := Task{ID: NewID(), Kind: kind.Name, ConversationID: conv.ID, Status: StatusQueued, Goal: goal,
		ApplicationID: in.ApplicationID, Access: access, Runtime: conv.Runtime, Unattended: in.Unattended,
		Profile: s.Store.Profile}
	files, err := WriteContext(conv.Cwd, t.ID, in.Context, s.Redact)
	if err != nil {
		return Task{}, err
	}
	if t.Prompt, err = BuildPrompt(kind, t, files); err != nil {
		return Task{}, err
	}
	t.Cwd = conv.Cwd
	if t.ContextHash, err = ContextHash(conv.Cwd, t.ID); err != nil {
		return Task{}, err
	}
	policy, err := s.Launcher.Policy(ctx)
	if err != nil {
		return Task{}, err
	}
	t.CapsHash = CapsHash(t, kind, policy)
	if t, err = s.Store.Insert(t); err != nil {
		return Task{}, err
	}
	if in.QueueOnly {
		return t, nil
	}
	return s.Start(ctx, t.ID)
}

func checkUnattended(kind Kind, access string, unattended bool) error {
	if !unattended {
		return nil
	}
	if FullAccess(access) {
		return errf(CodeUnattendedFull, "--unattended needs a task that can't change anything (--access qa): "+
			"the %s level has full access to this computer, and nobody would be there to see it", access)
	}
	if !kind.UnattendedOK {
		return errf(CodeInvalidInput, "kind %s can't run unattended", kind.Name)
	}
	return nil
}

// Start runs a queued task's turn (the person's click for a queued one).
// Starting a task that is running or over returns it unchanged. Being queued
// is not permission to run: start re-checks that coder mode is still on, that
// the context folder and the capabilities are what they were at submit, and
// refuses (leaving the task queued) when they are not.
func (s *Service) Start(ctx context.Context, id string) (Task, error) {
	t, err := s.Store.Get(id)
	if err != nil || t.Status != StatusQueued {
		return t, err
	}
	if err := s.verifyAtStart(ctx, t); err != nil {
		return Task{}, err
	}
	token := NewToken()
	if err := s.Store.SetToken(id, token); err != nil {
		return Task{}, err
	}
	moved, err := s.Store.move(id, []string{StatusQueued}, StatusRunning, nil)
	if err != nil {
		return Task{}, err
	}
	if !moved {
		return s.Store.Get(id)
	}
	if err := s.Launcher.Detach(ctx, id, token); err != nil {
		_, _ = s.Store.move(id, []string{StatusRunning}, StatusFailed, map[string]any{"error": clip(err.Error()), "token_hash": ""})
		return Task{}, err
	}
	return s.Store.Get(id)
}

func (s *Service) verifyAtStart(ctx context.Context, t Task) error {
	if err := s.Launcher.Ready(ctx); err != nil {
		return err
	}
	kind, err := Lookup(t.Kind)
	if err != nil {
		return err
	}
	if err := checkUnattended(kind, t.Access, t.Unattended); err != nil {
		return err
	}
	policy, err := s.Launcher.Policy(ctx)
	if err != nil {
		return err
	}
	if CapsHash(t, kind, policy) != t.CapsHash {
		return errf(CodeCapsChanged, "task %s was queued under other settings than are in force now (its access, kind or the coder settings changed): cancel it and submit it again", t.ID)
	}
	h, err := ContextHash(t.Cwd, t.ID)
	if err != nil {
		return err
	}
	if h != t.ContextHash {
		return errf(CodeContextChanged, "the context folder of task %s changed since it was queued: cancel the task and submit it again", t.ID)
	}
	return nil
}

// Complete records result as task id's result. Only the holder of token may,
// and only for a task that is running or waiting for the user; a result that
// does not match the kind's schema is refused with the reason.
func (s *Service) Complete(id, token string, result []byte) (Task, error) {
	t, err := s.Store.Get(id)
	if err != nil {
		return Task{}, err
	}
	if !s.Store.CheckToken(id, token) {
		return Task{}, errf(CodeBadToken, "this process does not hold task %s's token: only the task's own session may complete it", id)
	}
	if t.Status != StatusRunning && t.Status != StatusNeedsUser {
		return Task{}, closedError(t, "complete it")
	}
	if len(result) > MaxResultBytes {
		return Task{}, errf(CodeSchemaInvalid, "the result is larger than %d MB", MaxResultBytes>>20)
	}
	var v any
	if err := json.Unmarshal(result, &v); err != nil {
		return Task{}, &Error{Code: CodeSchemaInvalid, Message: "the result file is not valid JSON: " + err.Error(),
			Fields: map[string]any{"reason": err.Error()}}
	}
	kind, err := Lookup(t.Kind)
	if err != nil {
		return Task{}, err
	}
	schema := kind.Schema
	if schema == nil {
		schema = map[string]any{"type": "object"}
	}
	if err := Validate(schema, v); err != nil {
		return Task{}, &Error{Code: CodeSchemaInvalid, Fields: map[string]any{"reason": err.Error()},
			Message: "the result does not match the " + kind.Name + " schema: " + err.Error() + ". Fix the file and run complete again."}
	}
	compact, _ := json.Marshal(v)
	if s.Redact != nil {
		compact = []byte(s.Redact(string(compact)))
	}
	moved, err := s.Store.move(id, []string{StatusRunning, StatusNeedsUser}, StatusComplete, map[string]any{"result_json": string(compact), "error": ""})
	if err != nil {
		return Task{}, err
	}
	if !moved {
		cur, _ := s.Store.Get(id)
		return Task{}, closedError(cur, "complete it")
	}
	return s.Store.Get(id)
}

// Fail marks task id failed for reason; the same holder rule as Complete.
func (s *Service) Fail(id, token, reason string) (Task, error) {
	t, err := s.Store.Get(id)
	if err != nil {
		return Task{}, err
	}
	if !s.Store.CheckToken(id, token) {
		return Task{}, errf(CodeBadToken, "this process does not hold task %s's token: only the task's own session may fail it", id)
	}
	if strings.TrimSpace(reason) == "" {
		return Task{}, errf(CodeInvalidInput, "--reason is required")
	}
	if s.Redact != nil {
		reason = s.Redact(reason)
	}
	moved, err := s.Store.move(id, []string{StatusRunning, StatusNeedsUser}, StatusFailed, map[string]any{"error": clip(reason)})
	if err != nil {
		return Task{}, err
	}
	if !moved {
		return Task{}, closedError(t, "fail it")
	}
	return s.Store.Get(id)
}

// Cancel stops a task that is not over. Its token stops working with it.
func (s *Service) Cancel(id string) (Task, error) {
	t, err := s.Store.Get(id)
	if err != nil {
		return Task{}, err
	}
	moved, err := s.Store.move(id, []string{StatusQueued, StatusRunning, StatusNeedsUser}, StatusCancelled, nil)
	if err != nil {
		return Task{}, err
	}
	if !moved {
		return Task{}, closedError(t, "cancel it")
	}
	return s.Store.Get(id)
}

// EndTurn is what the process that ran a task's turn reports when the turn is
// over: a task still running was not completed, so it needs the user (with
// the turn's error, if it ended in one). A task that is already over stays.
func (s *Service) EndTurn(id, turnErr string) error {
	extra := map[string]any{}
	if turnErr != "" {
		if s.Redact != nil {
			turnErr = s.Redact(turnErr)
		}
		extra["error"] = clip(turnErr)
	}
	_, err := s.Store.move(id, []string{StatusRunning}, StatusNeedsUser, extra)
	return err
}

// Wait blocks until task id is over or needs the user, ctx ends, or timeout
// (when positive) passes. On a timeout it returns the task as it is with a
// CodeWaitTimeout error.
func (s *Service) Wait(ctx context.Context, id string, timeout time.Duration) (Task, error) {
	poll := s.PollEvery
	if poll <= 0 {
		poll = 500 * time.Millisecond
	}
	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}
	for {
		t, err := s.Store.Get(id)
		if err != nil || Terminal(t.Status) || t.Status == StatusNeedsUser {
			return t, err
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-deadline:
			return t, &Error{Code: CodeWaitTimeout, Message: "task " + id + " is still " + t.Status,
				Fields: map[string]any{"status": t.Status}}
		case <-time.After(poll):
		}
	}
}

func clip(s string) string {
	if len(s) <= maxErrorBytes {
		return s
	}
	return strings.ToValidUTF8(s[:maxErrorBytes], "") + "…"
}
