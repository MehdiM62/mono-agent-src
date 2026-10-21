//go:build unix

package extension

import (
	"os"
	"syscall"
)

// ownedByMe says whether the file belongs to the current user.
func ownedByMe(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
