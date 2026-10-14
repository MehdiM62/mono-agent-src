//go:build !windows

package clicore

import (
	"os/exec"
	"testing"
)

// DetachStart starts the command in a session of its own.
func TestDetachStartStartsANewSession(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("true")
	if err := DetachStart(cmd); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setsid {
		t.Fatalf("SysProcAttr = %+v, want Setsid", cmd.SysProcAttr)
	}
}
