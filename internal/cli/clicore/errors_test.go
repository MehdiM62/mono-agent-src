package clicore

import (
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/monomind"
)

// TestExitCodeFor_ProtocolErrorPreservesSpecificCode is the regression test
// for chat's os.Exit fix: RunE now returns a *monomind.ProtocolError instead
// of calling os.Exit(res.Err.ExitCode) directly, so ExitCodeFor must map it
// back to that same specific code (e.g. 124 timeout, 130 cancelled) rather
// than falling through to the generic default of 1.
func TestExitCodeFor_ProtocolErrorPreservesSpecificCode(t *testing.T) {
	cases := []struct {
		name string
		err  *monomind.ProtocolError
		want int
	}{
		{"timeout", &monomind.ProtocolError{Code: monomind.ErrTimeout, ExitCode: 124}, 124},
		{"cancelled", &monomind.ProtocolError{Code: monomind.ErrCancelled, ExitCode: 130}, 130},
		{"auth zero exit code", &monomind.ProtocolError{Code: monomind.ErrAuth, ExitCode: 0}, 0},
	}
	for _, c := range cases {
		if got := ExitCodeFor(c.err); got != c.want {
			t.Errorf("%s: ExitCodeFor = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestExitCodeFor_CliErrorStillWinsOverGenericDefault(t *testing.T) {
	if got := ExitCodeFor(ErrNotFound); got != 2 {
		t.Errorf("ExitCodeFor(ErrNotFound) = %d, want 2 (regression guard: adding the ProtocolError case must not shadow this)", got)
	}
}

func TestExitCodeFor_NilIsZero(t *testing.T) {
	if got := ExitCodeFor(nil); got != 0 {
		t.Errorf("ExitCodeFor(nil) = %d, want 0", got)
	}
}

// A command whose Deps never got its operator check refuses: an unwired
// check must not open what only the user does (#845 code review).
func TestOperatorRefusalFailsClosed(t *testing.T) {
	if why := OperatorRefusal(nil); !strings.Contains(why, "operator check is not wired") {
		t.Errorf("a nil check = %q, want a refusal that says it isn't wired", why)
	}
	if why := OperatorRefusal(func() string { return "" }); why != "" {
		t.Errorf("the user's process = %q, want none", why)
	}
	if why := OperatorRefusal(func() string { return "CLAUDECODE is set" }); why != "CLAUDECODE is set" {
		t.Errorf("an agent's process = %q", why)
	}
}

// A command whose Deps never got its RequireOperator refuses: an unwired
// check fails closed, as OperatorRefusal does.
func TestRequireOperatorFailsClosed(t *testing.T) {
	err := RequireOperator(nil, "approve a send")
	var fields interface{ JSONErrorFields() map[string]any }
	if ExitCodeFor(err) != 3 || !errors.As(err, &fields) || fields.JSONErrorFields()["code"] != "operator_only" ||
		!strings.Contains(err.Error(), "only the user can approve a send: the operator check is not wired") {
		t.Errorf("a nil check = %v", err)
	}
	if err := RequireOperator(func(string) error { return nil }, "approve a send"); err != nil {
		t.Errorf("the user's process = %v", err)
	}
	refused := errors.New("an agent")
	if err := RequireOperator(func(string) error { return refused }, "approve a send"); err != refused {
		t.Errorf("an agent's process = %v", err)
	}
}
