package tasks

import "fmt"

// sessionError is a refusal in a session's words. It matches the same sentinel as the
// personal board's refusal of the kind (errors.Is, so the exit codes and the codes of the
// JSON error document stay), but says what is wrong in the session's terms instead of
// repeating the sentinel's, which are about claiming a task of the personal board.
type sessionError struct {
	base error
	msg  string
}

func (e *sessionError) Error() string { return e.msg }
func (e *sessionError) Unwrap() error { return e.base }

// sessionNotReady is notReady for a session verb.
func sessionNotReady(format string, a ...any) error {
	return &sessionError{ErrNotReady, fmt.Sprintf(format, a...)}
}

// bindingRefused is operatorOnly for an agent that acts outside its binding: it is the
// agent's token that forbids it, not the operator's role that is missing.
func bindingRefused(format string, a ...any) error {
	return &sessionError{ErrOperatorOnly, fmt.Sprintf(format, a...)}
}
