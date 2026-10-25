//go:build !windows

package chat

import "syscall"

// A host-started turn is its own session leader (clicore.DetachStart), so its
// pid is its process group: signalling -pid reaches the agents under it too.
func terminateGroup(pid int) error { return syscall.Kill(-pid, syscall.SIGTERM) }

func killGroup(pid int) error { return syscall.Kill(-pid, syscall.SIGKILL) }
