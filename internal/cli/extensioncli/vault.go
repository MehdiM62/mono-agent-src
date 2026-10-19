package extensioncli

import (
	"context"
	"errors"
	"fmt"
	"slices"

	capturecli "github.com/monoes/mono-agent/internal/cli/capture"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/vault"
)

// The vault's half of the extension bridge (#451): a vault.save from the side
// panel's confirm card becomes an item in the image/file vault of the profile
// it names. newExtensionServer installs it on every bridge this binary builds.
// The extension package has already checked the request (size, type, name,
// client id); this checks that the profile is live and writes, once per client
// id (vault.SaveBytes). It never reads a stored file back.

// vaultSaveSource is the source every extension save is recorded with.
const vaultSaveSource = "extension"

// vaultSink saves vault.save requests into the vault of the database at path,
// opening it per request as boardSink does.
type vaultSink struct{ path string }

// extensionVaultSink is the sink newExtensionServer installs.
func extensionVaultSink(path string) extension.VaultSink { return vaultSink{path: path} }

// SaveToVault implements extension.VaultSink.
func (v vaultSink) SaveToVault(ctx context.Context, f extension.VaultFile) (extension.VaultSaved, error) {
	db, err := capturecli.OpenProfileDB(v.path)
	if err != nil {
		// A fixed message, as boardSink's: the reason may be shown in a page.
		return extension.VaultSaved{}, extension.Unavailable("MonoAgent has no database yet: start MonoAgent or run any monoagentcli command")
	}
	defer db.Close()
	// `extension serve` never migrates: a database from before vault.save has
	// no client_id column until some command has run. The bridge does not
	// migrate it either.
	var cols int
	err = db.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('vault_images') WHERE name = 'client_id'`).Scan(&cols)
	if err != nil {
		return extension.VaultSaved{}, extension.Unavailable("MonoAgent could not read its database; try again")
	}
	if cols == 0 {
		return extension.VaultSaved{}, extension.Unavailable("the monoagent database is too old for saves from the browser: run any monoagentcli command once to upgrade it")
	}
	// Only a live profile: one deleted since the panel listed it must not get
	// its vault folder back (the rule captures follow).
	profiles, err := profiledir.List(ctx, db.DB)
	if err != nil {
		return extension.VaultSaved{}, extension.Unavailable("MonoAgent could not read its profiles; try again")
	}
	if !slices.ContainsFunc(profiles, func(p profiledir.Profile) bool { return p.ID == f.ProfileID }) {
		return extension.VaultSaved{}, &extension.RequestError{Code: extension.CodeInvalidInput,
			Err: fmt.Errorf("profile %.64q does not exist (any more): choose another in the side panel", f.ProfileID)}
	}
	id, created, err := vault.SaveBytes(ctx, db.DB, f.ProfileID, vault.SaveInput{
		Data: f.Data, Ext: f.Ext, Label: f.Name, Source: vaultSaveSource, ClientID: f.ClientID, SourceURL: f.SourceURL,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return extension.VaultSaved{}, err
		}
		return extension.VaultSaved{}, &extension.RequestError{Code: extension.CodeInternal, Err: errors.New("MonoAgent could not save the file; try again")}
	}
	return extension.VaultSaved{ID: id, Created: created, Name: f.Name, MIME: f.MIME, SizeBytes: len(f.Data)}, nil
}
