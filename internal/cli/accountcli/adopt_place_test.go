package accountcli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/account/accounttest"
	"github.com/monoes/mono-agent/internal/library"
	"github.com/monoes/mono-agent/internal/library/libraryfake"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
)

// ownDB is a migrated database where this OS user's own install keeps it: HOME and the home
// directory the system gives the user are one temporary directory, and the database is its
// .monoagent/monoagent.db.
func ownDB(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	SetUserHomeForTest(t, home)
	path := filepath.Join(home, ".monoagent", "monoagent.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(testdb.Path(t), path); err != nil {
		t.Fatal(err)
	}
	return path
}

// placeRig is the user's own install as the adoption sees it, with a fake monoes.me taken for the
// real one: the database holds an older library login of the default profile, and nobody has
// signed in. Its tests install the process guard, so they run one at a time.
type placeRig struct {
	fake            *libraryfake.Server
	home, dbPath    string
	profile         string
	access, refresh string // the older login's tokens
	stderr          bytes.Buffer
}

func newPlaceRig(t *testing.T) *placeRig {
	t.Helper()
	r := &placeRig{fake: libraryfake.New(), dbPath: ownDB(t)}
	r.home = filepath.Dir(filepath.Dir(r.dbPath))
	t.Cleanup(r.fake.Close)
	libraryfake.TrustKey(t)
	account.SetHostForTest(t, r.fake.URL)
	prev := isProductionHost
	isProductionHost = func(h string) bool { return h == r.fake.URL }
	t.Cleanup(func() { isProductionHost = prev })
	account.SetSealerForTest(t, account.NewMemorySealer())
	account.SetEnforceFromForTest(t, time.Now().Add(-time.Hour))
	r.seed(t)
	g, err := account.NewDefaultGuard()
	if err != nil {
		t.Fatal(err)
	}
	account.InstallForTest(t, g)
	return r
}

// seed stores the older login in the default profile's vault, as the previous release did.
func (r *placeRig) seed(t *testing.T) {
	t.Helper()
	db := r.open(t)
	defer db.Close()
	ps, err := profiledir.List(context.Background(), db.DB)
	if err != nil || len(ps) == 0 || !ps[0].Default {
		t.Fatalf("no default profile: %v", err)
	}
	r.profile = ps[0].Name
	r.access, r.refresh = r.fake.NewGrant("ada")
	tok := &library.Token{AccessToken: r.access, RefreshToken: r.refresh, TokenType: "Bearer", Method: "pkce", BaseURL: r.fake.URL,
		User: &library.User{ID: "u-ada", Username: "ada", Email: "ada@example.com"}}
	if err := r.vault(db, ps[0].ID).Save(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
}

func (r *placeRig) open(t *testing.T) *storage.Database {
	t.Helper()
	db, err := storage.NewDatabase(r.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func (r *placeRig) vault(db *storage.Database, profileID string) *library.VaultStore {
	return &library.VaultStore{DB: db.DB, ProfileID: profileID, BaseURL: r.fake.URL}
}

// run is a gated command's first run on the database at dbPath.
func (r *placeRig) run(t *testing.T, dbPath string) {
	t.Helper()
	cmd := find(t, "workflow", "list")
	cmd.SetErr(&r.stderr)
	AdoptFirstRun(cmd, dbPath, library.AdoptIntoAccount)
}

// olderLogin is the older login's refresh token as the database at dbPath holds it, "" when gone,
// and whether the database has had its try.
func (r *placeRig) olderLogin(t *testing.T, dbPath string) (refresh string, claimed bool) {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if tok, _ := r.vault(db, "default").Load(context.Background()); tok != nil {
		refresh = tok.RefreshToken
	}
	v, _ := db.GetSetting(AdoptionSetting)
	return refresh, v != ""
}

// noTokens fails when the output holds a token of the older login or of the session.
func (r *placeRig) noTokens(t *testing.T) {
	t.Helper()
	secrets := []string{r.access, r.refresh}
	if st, err := account.DefaultStore(); err == nil {
		if sess, _ := st.Load(); sess != nil {
			secrets = append(secrets, sess.AccessToken)
		}
	}
	for _, s := range secrets {
		if s != "" && strings.Contains(r.stderr.String(), s) {
			t.Fatal("a token is in the output")
		}
	}
}

// #878: a copy of the user's database, or the database reached from a HOME made for a test, holds
// the user's real login, and the keyring would decrypt it. The adoption sends nothing, keeps the
// login, claims no try, and says why.
func TestAdoptionLeavesACopiedDatabaseAlone(t *testing.T) {
	for _, c := range []struct {
		name, why string
		place     func(t *testing.T, r *placeRig) string // moves the user's database or HOME; returns the path the command opens
	}{
		{"a HOME that is not the user's", "the home directory is", func(t *testing.T, r *placeRig) string {
			SetUserHomeForTest(t, t.TempDir())
			return r.dbPath
		}},
		{"the user's database from another HOME", "the home directory is", func(t *testing.T, r *placeRig) string {
			other := t.TempDir()
			t.Setenv("HOME", other)
			t.Setenv("USERPROFILE", other)
			return r.dbPath
		}},
		{"a copy beside the user's database", "the database is", func(t *testing.T, r *placeRig) string {
			cp := filepath.Join(r.home, "copy.db")
			b, err := os.ReadFile(r.dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cp, b, 0o600); err != nil {
				t.Fatal(err)
			}
			return cp
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newPlaceRig(t)
			path := c.place(t, r)
			r.run(t, path)
			r.run(t, path)
			r.noTokens(t)
			if n := len(r.fake.TokenRequests()); n != 0 {
				t.Fatalf("%d token requests reached monoes.me, want none", n)
			}
			if refresh, claimed := r.olderLogin(t, path); refresh != r.refresh || claimed {
				t.Fatalf("the older login kept %v, the try claimed %v: want it kept and no claim", refresh == r.refresh, claimed)
			}
			out := r.stderr.String()
			if strings.Count(out, "is not adopted") != 2 || !strings.Contains(out, c.why) {
				t.Fatalf("each run must say why it adopted nothing (%q): %q", c.why, out)
			}
		})
	}
}

// The upgrade path: the user's own install adopts its own older login, as before #878. The CLI's
// default --db-path is spelled with ~, and a HOME reached through a symlink is the same directory.
func TestAdoptionOnTheUsersOwnInstall(t *testing.T) {
	for _, c := range []struct {
		name string
		home func(t *testing.T, r *placeRig)
	}{
		{"HOME as the system gives it", func(*testing.T, *placeRig) {}},
		{"HOME through a symlink", func(t *testing.T, r *placeRig) {
			link := filepath.Join(t.TempDir(), "home")
			if err := os.Symlink(r.home, link); err != nil {
				t.Skip("no symlinks here:", err)
			}
			t.Setenv("HOME", link)
			t.Setenv("USERPROFILE", link)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newPlaceRig(t)
			c.home(t, r)
			r.run(t, "~/.monoagent/monoagent.db")
			r.noTokens(t)
			if r.fake.Refreshes != 1 {
				t.Fatalf("%d refresh grants, want 1", r.fake.Refreshes)
			}
			if refresh, claimed := r.olderLogin(t, r.dbPath); refresh != "" || !claimed {
				t.Fatalf("after the adoption the older login is still there %v, the try claimed %v", refresh != "", claimed)
			}
			want := "Signed in to " + r.fake.URL + " with the earlier library login of profile \"" + r.profile + "\"."
			if strings.TrimSpace(r.stderr.String()) != want {
				t.Fatalf("output %q, want %q", r.stderr.String(), want)
			}
		})
	}
}

// Whatever monoes.me answers, one line says what became of the older login, naming the host and
// the profile and never a token, so that an incident can be reconstructed.
func TestAdoptionSaysWhatBecameOfTheLogin(t *testing.T) {
	for _, c := range []struct {
		mode libraryfake.RefreshMode
		want string
	}{
		{libraryfake.RefreshOK, "Signed in to"},
		{libraryfake.RefreshInvalidGrant, "is no longer valid at"},
		{libraryfake.RefreshServerError, "was lost or could not be stored; the login was removed"},
		{libraryfake.RefreshInvalidClient, "did not take the earlier library login"},
	} {
		t.Run(c.want, func(t *testing.T) {
			r := newPlaceRig(t)
			r.fake.SetRefreshMode(c.mode)
			r.run(t, r.dbPath)
			r.noTokens(t)
			out := r.stderr.String()
			if strings.Count(out, "\n") != 1 || !strings.Contains(out, c.want) || !strings.Contains(out, r.fake.URL) ||
				!strings.Contains(out, "profile \""+r.profile+"\"") {
				t.Fatalf("output %q, want one line with %q, the host and the profile", out, c.want)
			}
		})
	}
}

// A HOME made for a test, with no older login in its database, is the common case: nothing is
// adopted, claimed or said.
func TestAdoptionIsQuietWithoutAnOlderLogin(t *testing.T) {
	accounttest.Install(t, accounttest.LockedNoLogin)
	account.SetEnforceFromForTest(t, time.Now().Add(48*time.Hour))
	path := ownDB(t)
	SetUserHomeForTest(t, t.TempDir())
	var errb bytes.Buffer
	cmd := find(t, "workflow", "list")
	cmd.SetErr(&errb)
	calls := 0
	AdoptFirstRun(cmd, path, counter(&calls))
	if calls != 0 || errb.Len() != 0 {
		t.Fatalf("%d tries, output %q: want none", calls, errb.String())
	}
}

// The user's home comes from the system's user database, not from $HOME, which a test or an agent
// sets to a directory of its own.
func TestUserHomeIsNotHOME(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	got, err := userHome()
	if err != nil {
		t.Skip("the system has no entry for this user:", err)
	}
	if got == "" || sameFile(got, home) {
		t.Fatalf("the user's home is %q, the HOME of the test", got)
	}
}
