//go:build !unix && !windows

package storage

import (
	"os"
	"syscall"
)

// processAlive reports whether pid is a running process.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}
