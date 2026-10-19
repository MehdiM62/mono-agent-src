package main

import (
	"path/filepath"
	"testing"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/library"
)

// #878, through the CLI's own wiring (its --db-path reaches the place check): with the fake
// monoes.me taken for the real one, `library logout` revokes the older login on the user's own
// install and leaves it alone in a HOME that is not the user's.
func TestLibraryLogoutRevokesTheOlderLoginOnlyOnTheUsersInstall(t *testing.T) {
	for _, own := range []bool{true, false} {
		t.Run(map[bool]string{true: "the user's install", false: "another HOME"}[own], func(t *testing.T) {
			noSeed(t)
			f := newLibFixture(t)
			account.SetProductionHostForTest(t, f.fake.URL)
			f.must(nil, "profile", "list") // makes the database
			dbPath := filepath.Join(f.home, ".monoagent", "monoagent.db")
			db, err := initDB(&globalConfig{DBPath: dbPath})
			if err != nil {
				t.Fatal(err)
			}
			seedOlderLogin(t, db, f.fake)
			db.Close()
			if own {
				account.SetUserHomeForTest(t, f.home)
			} else {
				account.SetUserHomeForTest(t, t.TempDir())
			}
			f.must(nil, "library", "logout")
			revoked := f.fake.Requests["POST /api/auth/oauth2/revoke"]
			db, err = initDB(&globalConfig{DBPath: dbPath})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			kept, _ := (&library.VaultStore{DB: db.DB, ProfileID: "default", BaseURL: f.fake.URL}).Exists(t.Context())
			if own && (revoked != 1 || kept) || !own && (revoked != 0 || !kept) {
				t.Fatalf("%d revocations, login kept %v", revoked, kept)
			}
		})
	}
}
