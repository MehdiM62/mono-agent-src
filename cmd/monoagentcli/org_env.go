package main

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/monoes/mono-agent/internal/cli/orgenv"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/orggrant"
	"github.com/monoes/mono-agent/internal/storage"
)

// The org environment and the org save helpers live in internal/cli/orgenv;
// these are the CLI's names for them.

// legacyOrgProjectRoot is where the CLI kept org state before the
// unification plan (C-31): orgs now resolve under the active profile's root,
// the same folder the GUI, chat tools, and org.run use. Orgs still found
// here are listed by `org legacy list` and moved by `org legacy move`.
const legacyOrgProjectRoot = "~/.monoagent"

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

func saveOrgReconciledFrom(ctx context.Context, db *storage.Database, profileID, root string, doc *orgdesign.Doc, opts orggrant.GenOptions, from string) (*orggrant.Report, error) {
	return orgenv.SaveReconciledFrom(ctx, db, profileID, root, doc, opts, from)
}

func reconcileOrgRows(ctx context.Context, db *storage.Database, profileID string, doc *orgdesign.Doc, opts orggrant.GenOptions) (*orggrant.Report, error) {
	return orgenv.ReconcileRows(ctx, db, profileID, doc, opts)
}

func lockOrgRows(root string) (release func(), err error) { return orgenv.LockRows(root) }

// printJSONValue marshals v and prints it as one line.
func printJSONValue(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return printOrgJSON(b)
}
