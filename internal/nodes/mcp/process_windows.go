//go:build windows

package mcpnode

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// setProcessGroup starts the server without a console window; on Windows
// proctree.Kill ends its tree with taskkill /T.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
