//go:build !windows

package account

import (
	"os"
	"syscall"
)

// folderID is dir's device and inode, "" when the system does not say. A rename on one filesystem
// keeps them; a copy, or a move to another filesystem, gets new ones.
func folderID(dir string) string {
	fi, err := os.Stat(dir)
	if err != nil {
		return ""
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return devIno(uint64(st.Dev), uint64(st.Ino))
}
