package org

import (
	"context"
	"database/sql"

	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/storage"
)

// What the rest of the CLI (the library, `status`) uses of the org commands'
// helpers.

// EnsureNewAutonomy gives an org created by an explicit command its
// starting autonomy row (ensureNewOrgAutonomy).
func EnsureNewAutonomy(ctx context.Context, db *storage.Database, profileID string, doc *orgdesign.Doc, by string) error {
	return ensureNewOrgAutonomy(ctx, db, profileID, doc, by)
}

// NameInUse reports the other profile (if any) whose folder already has an
// org named name (orgNameInUse).
func NameInUse(db *sql.DB, name, exceptProfile string) (string, error) {
	return orgNameInUse(db, name, exceptProfile)
}

// StatusDaemons is the `status --json` daemon block (statusDaemons).
func StatusDaemons(root string) map[string]interface{} { return statusDaemons(root) }
