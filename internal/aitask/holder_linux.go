//go:build linux

package aitask

import (
	"os"
	"strconv"
	"strings"
)

// ProcessWithin reports whether the calling process is pid or runs under it.
// A var so tests can stand in for the process tree.
var ProcessWithin = func(pid int) bool {
	for p, depth := os.Getpid(), 0; p > 1 && depth < 64; depth++ {
		if p == pid {
			return true
		}
		b, err := os.ReadFile("/proc/" + strconv.Itoa(p) + "/stat")
		if err != nil {
			return false
		}
		// pid (comm) state ppid ...: comm may hold spaces and ")".
		rest := string(b)
		rest = rest[strings.LastIndexByte(rest, ')')+1:]
		f := strings.Fields(rest)
		if len(f) < 2 {
			return false
		}
		if p, err = strconv.Atoi(f[1]); err != nil {
			return false
		}
	}
	return false
}
