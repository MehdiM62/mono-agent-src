package account

import (
	"fmt"
	"path/filepath"
	"strings"
)

// A sealed refresh token names the account folder it was sealed for (#878). The OS key store
// opens it in any copy of the folder that the same OS user makes, and a copy that renews or
// revokes it races the original, so it is read only in that folder: elsewhere it is as unreadable
// as a token whose key is missing, nothing is sent, and the copy has to sign in on its own. The
// name is sealed with the token, so it cannot be changed without the key.
const sealedForPrefix = "monoagent-account-v2\x00"

// sealFor is what is sealed for token in dir: the prefix, the folder's real path, a NUL, the token.
func sealFor(dir, token string) []byte {
	return []byte(sealedForPrefix + realDir(dir) + "\x00" + token)
}

// sealedFor is the token that plain holds for dir. One sealed for another folder is refused. One
// that a release before this sealed names no folder: it is read only in this OS user's own account
// folder (ownAccountDir), and the next refresh seals its successor for that folder.
func sealedFor(dir string, plain []byte) (string, error) {
	rest, named := strings.CutPrefix(string(plain), sealedForPrefix)
	if !named {
		if why := ownAccountDir(dir); why != "" {
			return "", fmt.Errorf("%w: the refresh token names no folder and %s; sign in here: monoagentcli account login", ErrKeyringUnavailable, why)
		}
		return string(plain), nil
	}
	folder, token, ok := strings.Cut(rest, "\x00")
	if !ok || token == "" {
		return "", fmt.Errorf("%w: the sealed refresh token is damaged", ErrKeyringUnavailable)
	}
	if !sameFile(folder, dir) {
		return "", fmt.Errorf("%w: the refresh token was sealed for %s, not %s, so this is a copy; sign in here: monoagentcli account login", ErrKeyringUnavailable, folder, dir)
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
