//go:build !windows

package mcpnode

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the server in its own process group so
// proctree.Kill reaches it and everything it starts.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
