package jevconf

import (
	"context"
	"database/sql"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/secrets"
	"github.com/monoes/mono-agent/internal/vault"
)

// seedKey stores a key under name in a vault scope and stamps its updated_at.
func seedKey(t *testing.T, db *sql.DB, scope, name, key, updatedAt string) string {
	t.Helper()
	id, err := secrets.Add(context.Background(), db, scope, "secret", name, map[string]string{"secret": key}, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE vault_secrets SET updated_at = ? WHERE id = ?`, updatedAt, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A key a profile vault holds is found from any profile once resolved: the
// newest of several is copied (not moved) to the machine vault.
func TestResolveKeyAdoptsMostRecentProfileKey(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TYPESAFE_API_KEY", "")
	db := newDB(t)
	ctx := context.Background()

	seedKey(t, db, "alice", "typesafe", "old-key", "2026-01-01T00:00:00Z")
	seedKey(t, db, "bob", "Jev Api key", "new-key", "2026-06-01T00:00:00Z")
	seedKey(t, db, "carol", "typesafe", "mid-key", "2026-03-01T00:00:00Z")

	if src, err := KeySource(ctx, db); err != nil || src != SourceProfileVault {
		t.Fatalf("KeySource before adoption = %q %v, want %q", src, err, SourceProfileVault)
	}
	k, src, err := ResolveKey(ctx, db, "")
	if err != nil || k != "new-key" || src != SourceVault {
		t.Fatalf("ResolveKey = %q %q %v", k, src, err)
	}
	if src, err := KeySource(ctx, db); err != nil || src != SourceVault {
		t.Fatalf("KeySource after adoption = %q %v, want %q", src, err, SourceVault)
	}

	// Every profile's own entry is untouched.
	for _, p := range []string{"alice", "bob", "carol"} {
		if es, _ := secrets.List(ctx, db, p); len(es) != 1 {
			t.Errorf("profile %s vault changed: %+v", p, es)
		}
	}
	if name, ok := VaultKeyName(ctx, db); !ok || name != SecretName {
		t.Errorf("machine entry = %q %v", name, ok)
	}

	// Idempotent, and removing the machine key does not resurrect a leftover.
	if copied, err := AdoptProfileKey(ctx, db); copied || err != nil {
		t.Fatalf("second adoption = %v %v", copied, err)
	}
	entries, _ := secrets.List(ctx, db, VaultScope)
	if err := secrets.Delete(ctx, db, VaultScope, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveKey(ctx, db, ""); err == nil {
		t.Fatal("a removed machine key was re-adopted from a profile")
	}
}

// An existing machine key is never replaced by a profile's.
func TestAdoptKeepsExistingMachineKey(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TYPESAFE_API_KEY", "")
	db := newDB(t)
	ctx := context.Background()
	seedKey(t, db, VaultScope, SecretName, "machine-key", "2026-01-01T00:00:00Z")
	seedKey(t, db, "alice", SecretName, "profile-key", "2026-09-01T00:00:00Z")

	k, _, err := ResolveKey(ctx, db, "")
	if err != nil || k != "machine-key" {
		t.Fatalf("ResolveKey = %q %v", k, err)
	}
	if es, _ := secrets.List(ctx, db, "alice"); len(es) != 1 {
		t.Fatalf("alice's entry was touched: %+v", es)
	}
}

// Every profile resolves the same machine key.
func TestResolveKeyIsMachineWide(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TYPESAFE_API_KEY", "")
	db := newDB(t)
	seedKey(t, db, VaultScope, SecretName, "machine-key", "2026-01-01T00:00:00Z")
	for _, profile := range []string{"a", "b"} {
		c, err := NewClient(context.Background(), db, profile, "", "", Capture)
		if err != nil || c == nil {
			t.Fatalf("profile %s: %v", profile, err)
		}
	}
}

// An explicit @secret: ref (a node's api_key) resolves from the machine vault
// first, then from the vault of the profile in ctx or passed to NewClient.
func TestResolveKeyExplicitRefFallsBackToProfileVault(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TYPESAFE_API_KEY", "")
	db := newDB(t)
	seedKey(t, db, "alice", "my-jev", "alice-key", "2026-01-01T00:00:00Z")
	seedKey(t, db, VaultScope, "shared", "machine-key", "2026-01-01T00:00:00Z")

	ctx := vault.ContextWithProfileID(context.Background(), "alice")
	if k, src, err := ResolveKey(ctx, db, "@secret:my-jev"); err != nil || k != "alice-key" || src != SourceVault {
		t.Fatalf("profile ref = %q %q %v", k, src, err)
	}
	if k, _, err := ResolveKey(ctx, db, "@secret:shared"); err != nil || k != "machine-key" {
		t.Fatalf("machine ref = %q %v", k, err)
	}
	other := vault.ContextWithProfileID(context.Background(), "bob")
	if _, _, err := ResolveKey(other, db, "@secret:my-jev"); err == nil {
		t.Fatal("another profile resolved alice's entry")
	}
	if c, err := NewClient(context.Background(), db, "alice", "@secret:my-jev", "", Capture); err != nil || c == nil {
		t.Fatalf("NewClient with a profile ref: %v", err)
	}
}

// A profile key that cannot be decrypted is attempted once, not on every call,
// and KeySource stops claiming a profile key after the attempt.
func TestUnreadableProfileKeyIsNotRetried(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TYPESAFE_API_KEY", "")
	db := newDB(t)
	ctx := context.Background()
	id := seedKey(t, db, "alice", SecretName, "k", "2026-01-01T00:00:00Z")
	if _, err := db.Exec(`UPDATE vault_secrets SET ciphertext = x'0102030405060708090a0b0c0d0e0f10' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if src, err := KeySource(ctx, db); err != nil || src != SourceProfileVault {
		t.Fatalf("before: %q %v", src, err)
	}
	if _, _, err := ResolveKey(ctx, db, ""); err == nil {
		t.Fatal("an undecryptable key resolved")
	}
	if v, _ := getSetting(db, adoptedSetting); v != "unreadable" {
		t.Fatalf("attempt not recorded: %q", v)
	}
	if _, err := KeySource(ctx, db); err == nil {
		t.Fatal("KeySource still reports an unreadable profile key")
	}
	if copied, err := AdoptProfileKey(ctx, db); copied || err != nil {
		t.Fatalf("retried: %v %v", copied, err)
	}
	if _, ok := VaultKeyName(ctx, db); ok {
		t.Fatal("machine vault got an entry")
	}
}

// KeySource follows ResolveKey: a pending profile key and a machine key come
// before env, which is the last resort.
func TestKeySourceOrder(t *testing.T) {
	keyring.MockInit()
	db := newDB(t)
	ctx := context.Background()
	t.Setenv("TYPESAFE_API_KEY", "env-key")
	if src, _ := KeySource(ctx, db); src != SourceEnv {
		t.Fatalf("env only: %q", src)
	}
	seedKey(t, db, "alice", SecretName, "k", "2026-01-01T00:00:00Z")
	if src, _ := KeySource(ctx, db); src != SourceProfileVault {
		t.Fatalf("pending adoption beats env: %q", src)
	}
	if _, src, _ := ResolveKey(ctx, db, ""); src != SourceVault {
		t.Fatalf("ResolveKey source = %q", src)
	}
	if src, _ := KeySource(ctx, db); src != SourceVault {
		t.Fatalf("after adoption: %q", src)
	}
}
