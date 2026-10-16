//go:build linux

package browserctl

import (
	"os"
	"strconv"
	"strings"
)

// processStart is when process pid started, as the kernel tells it:
// /proc/<pid>/stat field 22, clock ticks since boot, prefixed with this
// boot's id ("<boot id>:<ticks>"), as the same pid can start at the same
// tick after another boot. ok is false when no such process can be seen
// (gone, or another pid namespace) or the boot id can't be read.
func processStart(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || len(strings.TrimSpace(string(boot))) == 0 {
		return "", false
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	// The command name (field 2) is in parentheses and may hold spaces.
	s := string(raw)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", false
	}
	fields := strings.Fields(s[i+1:]) // from field 3 on
	if len(fields) < 20 {
		return "", false
	}
	return strings.TrimSpace(string(boot)) + ":" + fields[19], true
}
