package main

import (
	"context"
	"database/sql"
	"sync"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/org"
	"github.com/monoes/mono-agent/internal/cli/orgenv"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/storage"
)

// The org environment and the org save helpers live in internal/cli/orgenv;
// these are the CLI's names for them.

// orgEnv is orgenv.Env on the CLI's config: initDB opens the database and
// resolves --profile.
type orgEnv struct {
	cfg         *globalConfig
	projectFlag string

	once sync.Once
	e    *orgenv.Env
}

func (e *orgEnv) env() *orgenv.Env {
	e.once.Do(func() {
		e.e = orgenv.New(orgenv.Deps{
			OpenDB:            func() (*storage.Database, error) { return initDB(e.cfg) },
			ProfileID:         func() string { return e.cfg.ProfileID },
			OpenWorkflowStore: openWorkflowStore,
			Project:           func() string { return e.projectFlag },
		})
	})
	return e.e
}

// newOrgCmd is `org` (internal/cli/org) on the CLI's config.
func newOrgCmd(cfg *globalConfig) *cobra.Command { return org.NewCommand(orgDeps(cfg)) }

// orgDeps is org.Deps on cfg.
func orgDeps(cfg *globalConfig) org.Deps {
	return org.Deps{
		OpenDB:            func() (*storage.Database, error) { return initDB(cfg) },
		ProfileID:         func() string { return cfg.ProfileID },
		OpenWorkflowStore: openWorkflowStore,
		JSONOutput:        func() bool { return cfg.JSONOutput },
		NotFound:          errNotFound,
		InvalidInput:      errInvalidInput,
		Reported:          func(err error) error { return reportedError{err} },
		StdinIsTerminal:   stdinIsTerminal,
		Serving:           servingCommand,
		CapabilityProbe:   func(cmd *cobra.Command) (*monomind.CapabilitySet, error) { return capabilityProbe(cmd) },
	}
}

func (e *orgEnv) Root() string { return e.env().Root() }

func (e *orgEnv) Profile() (*storage.Database, string, string, error) { return e.env().Profile() }

func (e *orgEnv) Close() { e.env().Close() }

func (e *orgEnv) genOptions(profileID string) orggrant.GenOptions {
	return e.env().GenOptions(profileID)
}

func selfExecutable() string { return orgenv.SelfExecutable() }

const daemonAPIAddrSetting = orgenv.DaemonAPIAddrSetting

func orgAPIAddr(db *storage.Database) string { return orgenv.APIAddr(db) }

func saveOrgReconciled(ctx context.Context, db *storage.Database, profileID, root string, doc *orgdesign.Doc, opts orggrant.GenOptions) (*orggrant.Report, error) {
	return orgenv.SaveReconciled(ctx, db, profileID, root, doc, opts)
}

func lockOrgRows(root string) (release func(), err error) { return orgenv.LockRows(root) }

// The org helpers the library, the daemon and `status` use; the commands
// themselves are in internal/cli/org.

func ensureNewOrgAutonomy(ctx context.Context, db *storage.Database, profileID string, doc *orgdesign.Doc, by string) error {
	return org.EnsureNewAutonomy(ctx, db, profileID, doc, by)
}

func orgNameInUse(db *sql.DB, name, exceptProfile string) (string, error) {
	return org.NameInUse(db, name, exceptProfile)
}

func statusDaemons(root string) map[string]interface{} { return org.StatusDaemons(root) }

func saveOrgSigned(ctx context.Context, root, from string, doc *orgdesign.Doc) (string, *orgsign.Outcome, error) {
	return orgenv.SaveSigned(ctx, root, from, doc)
}

func saveOrgDoc(ctx context.Context, root string, doc *orgdesign.Doc) (string, error) {
	return orgenv.SaveDoc(ctx, root, doc)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
