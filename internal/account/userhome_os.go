//go:build (cgo || darwin || windows) && (!osusergo || windows)

package account

import "os/user"

// systemHome asks the system's user database through os/user, which in this build (cgo, macOS's
// libc, or Windows) never falls back to $HOME.
func systemHome() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}
