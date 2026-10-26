//go:build !windows

package chat

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// pidStamp says what makes pid the process it is now: its start time. "" when
// it cannot be read (the process is gone).
func pidStamp(pid int) string {
	if runtime.GOOS == "linux" {
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			return ""
		}
		s := string(raw)
		i := strings.LastIndex(s, ")")
		if i < 0 {
			return ""
		}
		f := strings.Fields(s[i+1:])
		if len(f) < 20 {
			return ""
		}
		return "start:" + f[19]
	}
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if s := strings.TrimSpace(string(out)); err == nil && s != "" {
		return "started:" + s
	}
	return ""
}
