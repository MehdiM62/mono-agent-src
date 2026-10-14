package jevconf

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/secrets"
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

func captureAdoptLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := adoptLog
	adoptLog = &buf
	t.Cleanup(func() { adoptLog = prev })
	return &buf
}

// A key a profile vault holds is found from any profile once resolved: it
// moves to the machine vault, the newest of several wins, the others stay.
func TestResolveKeyAdoptsMostRecentProfileKey(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TYPESAFE_API_KEY", "")
	db := newDB(t)
	ctx := context.Background()
	logBuf := captureAdoptLog(t)

	seedKey(t, db, "alice", "typesafe", "old-key", "2026-01-01T00:00:00Z")
	newest := seedKey(t, db, "bob", "Jev Api key", "new-key", "2026-06-01T00:00:00Z")
	seedKey(t, db, "carol", "typesafe", "mid-key", "2026-03-01T00:00:00Z")

	if src, err := KeySource(ctx, db); err != nil || src != SourceVault {
		t.Fatalf("KeySource before adoption = %q %v", src, err)
	}
	k, src, err := ResolveKey(ctx, db, "")
	if err != nil || k != "new-key" || src != SourceVault {
		t.Fatalf("ResolveKey = %q %q %v", k, src, err)
	}
	if !bytes.Contains(logBuf.Bytes(), []byte(`"bob"`)) {
		t.Errorf("the winner is not logged: %s", logBuf.String())
	}

	// The winner moved; the other profiles' entries are untouched.
	if es, _ := secrets.List(ctx, db, "bob"); len(es) != 0 {
		t.Errorf("winner still in bob's vault: %+v (id %s)", es, newest)
	}
	for _, p := range []string{"alice", "carol"} {
		if es, _ := secrets.List(ctx, db, p); len(es) != 1 {
			t.Errorf("profile %s vault changed: %+v", p, es)
		}
	}
	if name, ok := VaultKeyName(ctx, db); !ok || name != SecretName {
		t.Errorf("machine entry = %q %v", name, ok)
	}

	// Idempotent, and removing the machine key does not resurrect a leftover.
	logBuf.Reset()
	if moved, err := AdoptProfileKey(ctx, db); moved || err != nil || logBuf.Len() != 0 {
		t.Fatalf("second adoption = %v %v %q", moved, err, logBuf.String())
	}
	entries, _ := secrets.List(ctx, db, VaultScope)
	if err := secrets.Delete(ctx, db, VaultScope, entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveKey(ctx, db, ""); err == nil {
		t.Fatal("a removed machine key was re-adopted from another profile")
	}
}

// An existing machine key is never replaced by a profile's.
func TestAdoptKeepsExistingMachineKey(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TYPESAFE_API_KEY", "")
	db := newDB(t)
	ctx := context.Background()
	captureAdoptLog(t)
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
	captureAdoptLog(t)
	seedKey(t, db, VaultScope, SecretName, "machine-key", "2026-01-01T00:00:00Z")
	for _, profile := range []string{"a", "b"} {
		c, err := NewClient(context.Background(), db, profile, "", "", Capture)
		if err != nil || c == nil {
			t.Fatalf("profile %s: %v", profile, err)
		}
	}
}
