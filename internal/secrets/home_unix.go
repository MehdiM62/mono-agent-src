//go:build !windows

package secrets

import (
	"os"
	"path/filepath"
	"syscall"
)

// ownedHome reports whether dir is a real directory (not a symlink) directly under parent and
// owned by uid. On darwin a CGO_ENABLED=0 build has no /etc/passwd users, so the home comes from
// os.UserHomeDir (which trusts $HOME) only when it passes this: a $HOME pointing anywhere but a
// directory the user owns under /Users is refused.
func ownedHome(dir string, uid int, parent string) bool {
	if dir == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || filepath.Dir(dir) != parent {
		return false
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	return ok && int(sys.Uid) == uid
}
