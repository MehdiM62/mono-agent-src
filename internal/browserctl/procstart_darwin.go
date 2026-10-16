//go:build darwin

package browserctl

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// processStart is when process pid started, as the kernel tells it
// (kinfo_proc's p_starttime). ok is false when there is no such process.
func processStart(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return "", false
	}
	t := kp.Proc.P_starttime
	return strconv.FormatInt(int64(t.Sec), 10) + "." + strconv.FormatInt(int64(t.Usec), 10), true
}
