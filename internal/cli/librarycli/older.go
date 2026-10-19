package librarycli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/library"
	"github.com/monoes/mono-agent/internal/storage"
)

// olderLoginStore is the profile's older login for base, the one a release before the machine
// session left in its vault, or nil when it may not be presented from here (library.OlderLoginPath,
// #878): a copy of the user's database, or a HOME made for a test, holds the user's real login,
// and nothing here sends, refreshes or revokes it. olderRefused then says why. db is the command's
// database; the login is read from the file the check found, which is db's file unless db was
// opened under another name, so that a path changed after the check cannot swap another file in.
func (e *libEnv) olderLoginStore(db *storage.Database, base string) (library.TokenStore, error) {
	path := clicore.ExpandPath(e.deps.DBPath())
	own, why := library.OlderLoginPath(base, path)
	if why != "" {
		e.olderRefused = why
		return nil, nil
	}
	vdb := db
	if own != path {
		var err error
		if vdb, err = storage.NewDatabase(own); err != nil {
			return nil, fmt.Errorf("open database: %w", err)
		}
		e.vaultDB = vdb
	}
	return &library.VaultStore{DB: vdb.DB, ProfileID: e.deps.ProfileID(), BaseURL: base}, nil
}

// sayOlderLoginKept tells the user that `library logout` left the profile's older login alone,
// when it was not to be presented from here and the database holds one (names only are read).
func (e *libEnv) sayOlderLoginKept(cmd *cobra.Command, base string) {
	if e.olderRefused == "" || e.db == nil {
		return
	}
	vs := &library.VaultStore{DB: e.db.DB, ProfileID: e.deps.ProfileID(), BaseURL: base}
	if has, _ := vs.Exists(cmd.Context()); has {
		fmt.Fprintf(cmd.ErrOrStderr(), "The earlier library login of this profile is neither revoked at %s nor removed: %s.\n", base, e.olderRefused)
	}
}
