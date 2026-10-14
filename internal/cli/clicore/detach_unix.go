//go:build !windows

package clicore

import (
	"os/exec"
	"syscall"
)

// DetachStart starts cmd in its own session so it outlives this process
// and the app that ran it.
func DetachStart(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
