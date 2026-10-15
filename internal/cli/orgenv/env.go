// Package orgenv is what the CLI's org commands, the daemon and the workflow
// commands share about orgs: the org project root and profile (Env), the
// reconciled and signed save of an org file, the org rows lock, the API
// address endpoint URLs point at, and the grant tier refresh after a
// workflow save.
package orgenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/monoes/mono-agent/internal/daemonhb"
	"github.com/monoes/mono-agent/internal/orgdecide"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Deps is what an Env needs from the CLI.
type Deps struct {
	// OpenDB opens the database and resolves the profile (the CLI's initDB).
	OpenDB func() (*storage.Database, error)
	// ProfileID is the profile in effect; read after OpenDB, which resolves
	// --profile to an id.
	ProfileID func() string
	// OpenWorkflowStore opens the workflow store granted workflows load from.
	OpenWorkflowStore func(*storage.Database) *workflow.HybridWorkflowStore
	// Project is --project ("" when not given); read on every call.
	Project func() string
}

// Env resolves the org project root and, for DB-backed org commands, the
// profile and database. The DB opens lazily: plain monomind proxies with an
// explicit --project never touch it.
type Env struct {
	Deps

	once    sync.Once
	db      *storage.Database
	dbErr   error
	rootDir string
}

// New returns an Env on d.
func New(d Deps) *Env { return &Env{Deps: d} }

func (e *Env) open() {
	e.once.Do(func() {
		e.db, e.dbErr = e.OpenDB()
		if e.dbErr != nil {
			return
		}
		if err := profiledir.EnsureLayout(e.db.DB, e.ProfileID()); err != nil {
			e.dbErr = fmt.Errorf("preparing profile folder for %q: %w", e.ProfileID(), err)
			return
		}
		e.rootDir = profiledir.Root(e.db.DB, e.ProfileID())
	})
}

// RootErr is the project root org proxies run against: --project when given,
// otherwise the profile's root. When the database cannot be opened there is no
// root and the error says why; it is never the default profile's folder and
// never "" (filepath.Join("", ...) would land in the current directory).
func (e *Env) RootErr() (string, error) {
	if p := e.Project(); p != "" {
		return expandPath(p), nil
	}
	e.open()
	if e.dbErr != nil {
		return "", e.dbErr
	}
	return e.rootDir, nil
}

// Profile returns the database, profile id, and profile root for commands
// that read or write grants, endpoints, or autonomy rows. Those rows are
// keyed by profile, so an explicit --project pointing anywhere else is
// refused instead of silently editing one folder's org under another
// profile's grants (C-31).
func (e *Env) Profile() (*storage.Database, string, string, error) {
	e.open()
	if e.dbErr != nil {
		return nil, "", "", e.dbErr
	}
	if p := e.Project(); p != "" {
		want, _ := filepath.Abs(e.rootDir)
		got, _ := filepath.Abs(expandPath(p))
		if filepath.Clean(want) != filepath.Clean(got) {
			return nil, "", "", fmt.Errorf("--project %s is not the org root of profile %q (%s); grants and autonomy belong to a profile — drop --project or pass --profile", got, e.ProfileID(), want)
		}
	}
	return e.db, e.ProfileID(), e.rootDir, nil
}

// Close closes the database when it was opened.
func (e *Env) Close() {
	if e.db != nil {
		e.db.Close()
	}
}

// GenOptions returns what reconcile needs to write generated org JSON
// blocks for this process.
func (e *Env) GenOptions(profileID string) orggrant.GenOptions {
	return orggrant.GenOptions{
		ProfileID: profileID,
		CLIPath:   SelfExecutable(),
		APIAddr:   APIAddr(e.db),
		Workflow:  GrantWorkflowLoader(e.db, e.OpenWorkflowStore),
	}
}

// expandPath expands ~ to the user's home directory (the CLI's expandPath).
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// SelfExecutable is the absolute, symlink-resolved path of this binary —
// the command monomind spawns for granted tool providers.
func SelfExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return "monoagentcli"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
}

// DaemonAPIAddrSetting is the settings key `monoagentcli daemon` records
// its --api-addr under, so endpoint URLs written while the daemon is down
// still point where it will listen.
const DaemonAPIAddrSetting = "daemon_api_addr"

// APIAddr is the address endpoint URLs point at: MONOAGENT_API_ADDR,
// else a live daemon's heartbeat, else the address the daemon last
// recorded, else the default.
func APIAddr(db *storage.Database) string {
	if v := os.Getenv("MONOAGENT_API_ADDR"); v != "" {
		return v
	}
	if hb, ok := daemonhb.Read(); ok && hb.APIAddr != "" {
		return hb.APIAddr
	}
	if db != nil {
		var v string
		if err := db.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, DaemonAPIAddrSetting).Scan(&v); err == nil && v != "" {
			return v
		}
	}
	return orggrant.DefaultAPIAddr
}

// SaveReconciled reconciles doc against the enforcement rows and saves
// it. Every CLI path that writes an org JSON goes through here, so a doc
// never reaches disk carrying grants or providers no row backs. The save
// keeps the org's operator signature (SaveSigned).
func SaveReconciled(ctx context.Context, db *storage.Database, profileID, root string, doc *orgdesign.Doc, opts orggrant.GenOptions) (*orggrant.Report, error) {
	return SaveReconciledFrom(ctx, db, profileID, root, doc, opts, doc.Name)
}

// SaveReconciledFrom is SaveReconciled with SaveSigned's from
// (the org file doc replaces; a rename's old name).
func SaveReconciledFrom(ctx context.Context, db *storage.Database, profileID, root string, doc *orgdesign.Doc, opts orggrant.GenOptions, from string) (*orggrant.Report, error) {
	rep, err := ReconcileRows(ctx, db, profileID, doc, opts)
	if err != nil {
		return rep, err
	}
	_, out, err := SaveSigned(ctx, root, from, doc)
	if err != nil {
		return rep, err
	}
	WarnSignature(out)
	return rep, nil
}

// ReconcileRows is SaveReconciled without the save: it rewrites
// doc's generated blocks from the grant, endpoint, and autonomy rows (and
// lowers the enforced autonomy to match the document), leaving the write to
// the caller.
func ReconcileRows(ctx context.Context, db *storage.Database, profileID string, doc *orgdesign.Doc, opts orggrant.GenOptions) (*orggrant.Report, error) {
	rep, err := orggrant.Reconcile(ctx, orggrant.NewStore(db.DB), doc, opts)
	if err != nil {
		return nil, err
	}
	auto, err := orgdecide.ReconcileAutonomy(ctx, orgdecide.NewStore(db.DB), profileID, doc)
	if err != nil {
		return nil, err
	}
	if auto.Lowered {
		rep.Findings = append(rep.Findings, orggrant.Finding{Kind: "autonomy_lowered", Detail: "lowered the enforced autonomy level to match the org file"})
	}
	if auto.Ignored != "" {
		rep.Findings = append(rep.Findings, orggrant.Finding{Kind: "autonomy_raise_ignored", Detail: auto.Ignored})
	}
	return rep, nil
}

// ReconcileOutcome is what reconciling one org file did — the daemon
// logs it, `org reconcile` prints it.
type ReconcileOutcome struct {
	Org      string             `json:"org"`
	Saved    bool               `json:"saved"`
	Findings []orggrant.Finding `json:"findings"`
	Error    string             `json:"error,omitempty"`
	// Signature is what the save did to the org's operator signature
	// (monomind 2.21+): re-signed, or left for review with a notice.
	Signature *orgsign.Outcome `json:"signature,omitempty"`
}

// RowsLockWait bounds how long a writer waits for the org rows lock; a holder keeps it for one
// load-write-save, so a longer wait means a stuck process, and failing beats hanging.
const RowsLockWait = 15 * time.Second

// LockRows serialises the commands that write an enforcement row and then the org file with the
// daemon's reconcile of that file (reconcileDoc). Without it the daemon can read the file between
// the two writes, see a live row the file does not back yet, and revoke it (C-3): `org grant add`
// then reports a grant that is already gone. Held until release is called.
func LockRows(root string) (release func(), err error) {
	path := filepath.Join(root, ".monomind", "org-rows.lock")
	deadline := time.Now().Add(RowsLockWait)
	for {
		release, err = daemonhb.LockFile(path)
		if !errors.Is(err, daemonhb.ErrHeld) {
			return release, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another process is writing this profile's org files (lock %s); try again", path)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
