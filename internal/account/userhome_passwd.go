//go:build !((cgo || darwin || windows) && (!osusergo || windows))

package account

import (
	"os"
	"strconv"
)

// systemHome reads /etc/passwd itself. In this build (no cgo, as the release CLI is built for
// Linux) os/user takes $HOME for a user that file has no entry for (an LDAP account, a container's
// arbitrary uid), so such a user has no home directory here, and OwnHome refuses it.
func systemHome() (string, error) {
	return homeFromPasswd("/etc/passwd", strconv.Itoa(os.Getuid()))
}
