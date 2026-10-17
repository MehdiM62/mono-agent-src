package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/secrets"
)

// An MCP caller is an AI: node_run of vault.secret_get, whose output is a
// vault entry's decrypted fields, is refused, and the value never comes back
// (the secret tools return names and metadata only).
func TestNodeRunRefusesToDecryptAVaultEntry(t *testing.T) {
	keyring.MockInit()
	s := newTestServerAllowMutations(t, true)
	rt, err := s.runtime()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.Add(context.Background(), rt.db.DB, testProfile, "secret", "kept", map[string]string{"secret": "vsecret-777"}, "", "", ""); err != nil {
		t.Fatal(err)
	}
	text, err := callNamed(t, s, "node_run", map[string]interface{}{"type": "vault.secret_get", "config": map[string]interface{}{"name": "kept"}})
	if err == nil || !strings.Contains(err.Error(), "vault.secret_get decrypts a vault entry") || strings.Contains(text, "vsecret-777") {
		t.Fatalf("node_run vault.secret_get: %v\n%s", err, text)
	}
	if list, err := callNamed(t, s, "secret_list", map[string]interface{}{}); err != nil || !strings.Contains(list, "kept") || strings.Contains(list, "vsecret-777") {
		t.Fatalf("secret_list: %v\n%s", err, list)
	}
}
