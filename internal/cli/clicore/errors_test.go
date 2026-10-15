package clicore

import (
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
