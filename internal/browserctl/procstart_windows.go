//go:build windows

package browserctl

import (
	"strconv"

	"golang.org/x/sys/windows"
)

// stillActive is GetExitCodeProcess's STILL_ACTIVE: not exited yet.
const stillActive = 259

// processStart is when process pid started, as the kernel tells it
// (GetProcessTimes' creation time). It is wall-clock time, so unlike
// Linux's ticks since boot it doesn't repeat after a reboot. ok is false
// when there is no such running process, or it can't be opened.
func processStart(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != stillActive {
		return "", false
	}
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return "", false
	}
	return strconv.FormatInt(created.Nanoseconds(), 10), true
}
