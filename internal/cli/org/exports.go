package org

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/cli/orgenv"
	"github.com/monoes/mono-agent/internal/orgdesign"
	"github.com/monoes/mono-agent/internal/storage"
)

// The org subcommands this package has so far, and the helpers the org
// commands still in package main use. e is the org environment of the
// `org` command they are added to, so they share its --project, database
// and profile.

// NewAutomationCommand is `org automation`.
func NewAutomationCommand(d Deps, e *orgenv.Env) *cobra.Command {
	return newOrgAutomationCmd(newEnv(d, e))
}

// NewAutomationRoleCommand is `org automation-role`.
func NewAutomationRoleCommand(d Deps, e *orgenv.Env) *cobra.Command {
	return newOrgAutomationRoleCmd(newEnv(d, e))
}

// NewGrantCommand is `org grant`.
func NewGrantCommand(d Deps, e *orgenv.Env) *cobra.Command { return newOrgGrantCmd(newEnv(d, e)) }

// NewEffectiveToolsCommand is `org effective-tools`.
func NewEffectiveToolsCommand(d Deps, e *orgenv.Env) *cobra.Command {
	return newOrgEffectiveToolsCmd(newEnv(d, e))
}

// NewAutonomyCommand is `org autonomy`.
func NewAutonomyCommand(d Deps, e *orgenv.Env) *cobra.Command { return newOrgAutonomyCmd(newEnv(d, e)) }

// NewSummaryCommand is `org summary`.
func NewSummaryCommand(d Deps, e *orgenv.Env) *cobra.Command { return newOrgSummaryCmd(newEnv(d, e)) }

// NewGroupCommand is `org group`.
func NewGroupCommand(d Deps, e *orgenv.Env) *cobra.Command { return newOrgGroupCmd(newEnv(d, e)) }

// NewReconcileDocCommand is `org reconcile-doc`.
func NewReconcileDocCommand(d Deps, e *orgenv.Env) *cobra.Command {
	return newOrgReconcileDocCmd(newEnv(d, e))
}

// LoadForEdit loads org name from the profile's org root, with the
// profile's database and id.
func LoadForEdit(e *orgenv.Env, name string) (*storage.Database, string, string, *orgdesign.Doc, error) {
	return loadOrgForEdit(newEnv(Deps{}, e), name)
}

// EnsureNewAutonomy gives an org created by an explicit command its
// starting autonomy row (ensureNewOrgAutonomy).
func EnsureNewAutonomy(ctx context.Context, db *storage.Database, profileID string, doc *orgdesign.Doc, by string) error {
	return ensureNewOrgAutonomy(ctx, db, profileID, doc, by)
}

// ChildGoalWarnings is childGoalWarnings.
func ChildGoalWarnings(root, name string) []string { return childGoalWarnings(root, name) }

// DocCarriesEnforcedKeys reports whether saving d needs the grant store.
func DocCarriesEnforcedKeys(d *orgdesign.Doc) bool { return docCarriesEnforcedKeys(d) }

// FileSHA256 is the hex SHA-256 of the file at path.
func FileSHA256(path string) (string, error) { return fileSHA256(path) }
