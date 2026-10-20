package secret

import (
	"strings"
	"testing"
)

// secret rebind is the user's own: an agent that copied the database cannot adopt it.
func TestRebind_OperatorOnly(t *testing.T) {
	dbPath := newSecretCLITestDB(t)
	t.Setenv("CLAUDECODE", "1")
	_, err := runSecretCmd(t, dbPath, "rebind")
	if err == nil || !strings.Contains(err.Error(), "only the user") {
		t.Fatalf("an agent: %v", err)
	}
	t.Setenv("CLAUDECODE", "")
	if _, err := runSecretCmd(t, dbPath, "rebind"); err == nil || strings.Contains(err.Error(), "only the user") {
		t.Fatalf("the operator is refused, or got no vault-key error: %v", err)
	}
}
