//go:build windows

package chat

import "os"

func terminateGroup(pid int) error { return killGroup(pid) }

func killGroup(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
