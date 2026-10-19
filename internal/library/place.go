package library

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/monoes/mono-agent/internal/account"
)

// OlderLoginPath says where the older login for host may be read from, given the database a
// command was pointed at (dbPath, expanded), or why it may not be read at all. An older library
// login in a profile's vault is the user's real monoes.me login, and the OS key store opens it in
// any copy of the database that the same OS user makes, so it is presented to monoes.me
// (exchanged, refreshed, sent with a request or revoked) only from this OS user's own install
// (#878). For a loopback host (account.IsProductionHost) that is dbPath. For any other host it is
// this OS user's own .monoagent/monoagent.db (account.OwnHome), and only when dbPath is that file:
// a copy of the database anywhere else, or a HOME made for a test, holds a login that is not its
// to present. The caller reads from the path returned, not from dbPath, so that a path changed
// after the check cannot swap another file in.
func OlderLoginPath(host, dbPath string) (path, why string) {
	if !account.IsProductionHost(host) {
		return dbPath, ""
	}
	own, why := account.OwnHome()
	if why != "" {
		return "", why
	}
	want := filepath.Join(own, ".monoagent", "monoagent.db")
	if !sameFile(dbPath, want) {
		return "", fmt.Sprintf("the database is %s, not %s", dbPath, want)
	}
	return want, ""
}

// sameFile says whether a and b are one file, however each is spelled.
func sameFile(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}
