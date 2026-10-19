package account

import (
	"fmt"
	"path/filepath"
	"strings"
)

// A sealed refresh token names the account folder it was sealed for (#878): its path, and its
// device and inode (folderID). The OS key store opens it in any copy of the folder that the same
// OS user makes, and a copy that renews or revokes it races the original, so it is read only in
// that folder: one at the same path, or the same folder renamed or moved on its filesystem. A copy
// gets a new inode, so elsewhere the token is as unreadable as one whose key is missing, nothing
// is sent, and the copy has to sign in on its own. The name is sealed with the token, so it cannot
// be changed without the key.
const (
	sealedFamily    = "monoagent-account-"
	sealedForPrefix = sealedFamily + "v3\x00"
)

// sealFor is what is sealed for token in dir: the prefix, the folder's real path, its device and
// inode, and the token, NUL-separated.
func sealFor(dir, token string) []byte {
	return []byte(sealedForPrefix + realDir(dir) + "\x00" + folderID(dir) + "\x00" + token)
}

// sealedFor is the token that plain holds for dir. One sealed for another folder is refused, and
// so is one of another version of this format (a build of this branch before v3 named the path
// only). One that a release before this sealed names no folder: it is read only in this OS user's
// own account folder (ownAccountDir), and the next refresh seals its successor for that folder.
func sealedFor(dir string, plain []byte) (string, error) {
	s := string(plain)
	rest, named := strings.CutPrefix(s, sealedForPrefix)
	switch {
	case !named && strings.HasPrefix(s, sealedFamily):
		return "", fmt.Errorf("%w: the refresh token was saved in a format this build does not read; sign in once more: monoagentcli account login", ErrKeyringUnavailable)
	case !named:
		if why := ownAccountDir(dir); why != "" {
			return "", fmt.Errorf("%w: the refresh token was saved by an earlier release and %s, so it cannot be told from a copy; sign in once more: monoagentcli account login", ErrKeyringUnavailable, why)
		}
		return s, nil
	}
	f := strings.SplitN(rest, "\x00", 3)
	if len(f) != 3 || f[2] == "" {
		return "", fmt.Errorf("%w: the sealed refresh token is damaged", ErrKeyringUnavailable)
	}
	folder, id, token := f[0], f[1], f[2]
	if !sameFile(folder, dir) && (id == "" || id != folderID(dir)) {
		return "", fmt.Errorf("%w: the refresh token was sealed in %s, and %s is a copy of it or on another disk; sign in once more: monoagentcli account login", ErrKeyringUnavailable, folder, dir)
	}
	return token, nil
}

// realDir is dir as an absolute path with its symlinks resolved, as far as it exists.
func realDir(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	return abs
}

// ownAccountDir says why dir is not this OS user's own account folder, "" when it is.
func ownAccountDir(dir string) string {
	own, why := OwnHome()
	if why != "" {
		return why
	}
	if want := filepath.Join(own, ".monoagent", "account"); !sameFile(dir, want) {
		return fmt.Sprintf("the account folder is %s, not %s", dir, want)
	}
	return ""
}
