package jevconf

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/jev"
	"github.com/monoes/mono-agent/internal/logging"
	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/vault"
)

// The TypeSafe key belongs to the machine, not to a profile: it is stored once,
// encrypted, in the vault under the reserved scope VaultScope. (The settings
// table is plaintext, so it is not used for the key.) jev_usage and
// jev_suggestions stay per-profile.

// VaultScope is the reserved vault profile id that holds machine-wide secrets.
// No profile may use it (profiledir.ValidProfileID, `profile create` and
// --profile reject it); it never appears in a profile's own vault.
const VaultScope = profiledir.MachineProfileID

// SecretName is the vault entry the resolver looks up.
const SecretName = "typesafe"

// Key sources reported by ResolveKey and KeySource.
const (
	SourceConfig = "config"
	SourceVault  = "vault"
	SourceEnv    = "env"
	// SourceProfileVault: a profile vault holds a readable-looking key that
	// has not been copied to the machine vault yet (the next ResolveKey
	// copies it).
	SourceProfileVault = "profile-vault"
)

// adoptedSetting records that the one-time copy of a profile's key to the
// machine vault has been attempted ("1": done or not needed; "unreadable":
// the candidates could not be decrypted). Either way it is not repeated, so
// an undecryptable key never re-prompts the keyring on every call, and a key
// removed from the machine vault on purpose is never re-adopted. `jev key
// set` and `jev key remove` set it too (MarkAdopted).
const adoptedSetting = "jev.key_adopted"

// MarkAdopted records that the profile-to-machine key adoption must not run.
func MarkAdopted(db *sql.DB) error { return setSetting(db, adoptedSetting, "1") }

func adoptionAttempted(db *sql.DB) bool {
	v, ok := getSetting(db, adoptedSetting)
	return ok && v != ""
}

// ResolveKey implements plan D3: an explicit value (not an @secret: ref) →
// the machine vault entry "typesafe" → TYPESAFE_API_KEY → jev.ErrNoAPIKey.
// An explicit @secret:<name> ref is resolved from the machine vault, then
// from the vault of the profile in ctx (vault.ProfileIDFromContext) when one
// is known; if both fail the env var still applies. A literal "@secret:…" is
// never returned.
//
// With no explicit value and no machine key yet, a key held by a profile's
// vault is first copied to the machine vault (AdoptProfileKey).
func ResolveKey(ctx context.Context, db *sql.DB, explicit string) (key, source string, err error) {
	return resolveKey(ctx, db, vault.ProfileIDFromContext(ctx), explicit)
}

func resolveKey(ctx context.Context, db *sql.DB, profileID, explicit string) (key, source string, err error) {
	explicit = strings.TrimSpace(explicit)
	ref := "@secret:" + SecretName
	if explicit != "" && !strings.HasPrefix(explicit, "@secret:") {
		return explicit, SourceConfig, nil
	}
	if explicit != "" {
		ref = explicit
	}
	var vaultErr error
	if db != nil {
		if explicit == "" {
			if _, err := AdoptProfileKey(ctx, db); err != nil {
				vaultErr = err
			}
			if name, ok := vaultKeyName(ctx, db, VaultScope); ok {
				ref = "@secret:" + name
			}
		}
		scopes := []string{VaultScope}
		if explicit != "" && profileID != "" && profileID != VaultScope {
			scopes = append(scopes, profileID)
		}
		for _, scope := range scopes {
			v, err := secrets.Resolve(ctx, db, scope, ref)
			if err == nil && v != "" && !strings.HasPrefix(v, "@secret:") {
				return v, SourceVault, nil
			}
			if vaultErr == nil {
				vaultErr = err
			}
		}
	}
	if v := strings.TrimSpace(envKey()); v != "" {
		return v, SourceEnv, nil
	}
	if vaultErr != nil {
		return "", "", fmt.Errorf("%w (vault: %v)", jev.ErrNoAPIKey, vaultErr)
	}
	return "", "", jev.ErrNoAPIKey
}

// KeySource reports where ResolveKey would find the key, in its order: the
// machine vault (a "typesafe" entry exists), else a profile vault whose key
// the next ResolveKey would copy (SourceProfileVault: adoption not yet
// attempted and a candidate with stored fields exists), else env. It never
// decrypts, so health checks never trigger a keyring or passphrase prompt.
func KeySource(ctx context.Context, db *sql.DB) (string, error) {
	if db != nil {
		if _, ok := vaultKeyName(ctx, db, VaultScope); ok {
			return SourceVault, nil
		}
		if !adoptionAttempted(db) && len(profileKeyCandidates(ctx, db)) > 0 {
			return SourceProfileVault, nil
		}
	}
	if strings.TrimSpace(envKey()) != "" {
		return SourceEnv, nil
	}
	return "", jev.ErrNoAPIKey
}

// VaultKeyName is the machine vault entry the key resolves from, if any.
func VaultKeyName(ctx context.Context, db *sql.DB) (string, bool) {
	return vaultKeyName(ctx, db, VaultScope)
}

// keyNameAliases are vault entry names, normalised (lower case, no spaces,
// dashes or underscores), accepted for the key when no entry is named
// exactly "typesafe" — people name the secret after the product or the model.
var keyNameAliases = map[string]bool{
	"typesafe": true, "typesafeapikey": true, "typesafekey": true, "typesafeapi": true,
	"jev": true, "jevapikey": true, "jevkey": true, "jevapi": true,
}

func normaliseKeyName(n string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(strings.ToLower(strings.TrimSpace(n)))
}

// vaultKeyName returns the entry of scope holding the TypeSafe key: exactly
// "typesafe" if present, else the single entry whose normalised name is a
// known alias. Two or more alias matches are ambiguous and ignored.
func vaultKeyName(ctx context.Context, db *sql.DB, scope string) (string, bool) {
	e, ok := vaultKeyEntry(ctx, db, scope)
	return e.Name, ok
}

func vaultKeyEntry(ctx context.Context, db *sql.DB, scope string) (secrets.Entry, bool) {
	entries, err := secrets.List(ctx, db, scope)
	if err != nil {
		return secrets.Entry{}, false
	}
	var alias []secrets.Entry
	for _, e := range entries {
		if e.Name == SecretName {
			return e, true
		}
		if keyNameAliases[normaliseKeyName(e.Name)] {
			alias = append(alias, e)
		}
	}
	if len(alias) == 1 {
		return alias[0], true
	}
	return secrets.Entry{}, false
}

// profileKeyCandidates lists the key entries held by profile vaults (never the
// machine scope) that have stored fields, most recently updated first; ties
// break on profile id.
func profileKeyCandidates(ctx context.Context, db *sql.DB) []secrets.Entry {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT profile_id FROM vault_secrets WHERE profile_id <> ?`, VaultScope)
	if err != nil {
		return nil
	}
	var scopes []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			scopes = append(scopes, p)
		}
	}
	rows.Close()
	var out []secrets.Entry
	for _, p := range scopes {
		if e, ok := vaultKeyEntry(ctx, db, p); ok && e.FieldCount > 0 {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ProfileID < out[j].ProfileID
	})
	return out
}

// AdoptProfileKey copies a TypeSafe key held by a profile's vault into the
// machine vault; the profile's own entry is left in place (workflows may
// reference it). It runs at most once per machine (see adoptedSetting):
// nothing happens when an attempt was made before, when the machine vault
// already has a key, or when no profile holds one. When several profiles hold
// a key the most recently updated readable one wins and the choice is logged.
// If no candidate can be decrypted the attempt is recorded and not repeated
// until `jev key set`.
func AdoptProfileKey(ctx context.Context, db *sql.DB) (copied bool, err error) {
	if adoptionAttempted(db) {
		return false, nil
	}
	if _, ok := vaultKeyName(ctx, db, VaultScope); ok {
		_ = MarkAdopted(db)
		return false, nil
	}
	cands := profileKeyCandidates(ctx, db)
	if len(cands) == 0 {
		_ = MarkAdopted(db)
		return false, nil
	}
	for _, c := range cands {
		fields, _, derr := secrets.DecryptFields(ctx, db, c.ProfileID, c.ID)
		key := strings.TrimSpace(fields["secret"])
		if key == "" && len(fields) == 1 {
			for _, v := range fields {
				key = strings.TrimSpace(v)
			}
		}
		if derr != nil || key == "" {
			err = fmt.Errorf("profile %q key entry %q unreadable: %v", c.ProfileID, c.Name, derr)
			continue
		}
		if _, aerr := secrets.Add(ctx, db, VaultScope, "secret", SecretName, map[string]string{"secret": key}, "", "", "TypeSafe Jev API key"); aerr != nil {
			if _, ok := vaultKeyName(ctx, db, VaultScope); ok {
				_ = MarkAdopted(db)
				return false, nil // another process adopted first
			}
			return false, fmt.Errorf("storing the TypeSafe key in the machine vault: %w", aerr)
		}
		_ = MarkAdopted(db)
		var others []string
		for _, o := range cands {
			if o.ID != c.ID {
				others = append(others, o.ProfileID)
			}
		}
		logging.FromContext(ctx).Info("jev: copied the TypeSafe key of a profile to the machine vault",
			"profile", c.ProfileID, "candidates", len(cands), "left_alone", others)
		return true, nil
	}
	_ = setSetting(db, adoptedSetting, "unreadable")
	return false, err
}
