package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/storage"
)

func TestProfileContextCommandAndLeadPrompt(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	custom, _ := filepath.EvalSymlinks(t.TempDir())
	addTestProfile(t, dbPath, "work", "Work Stuff", custom)
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.DB.Exec(`INSERT INTO vault_secrets (id, seq, profile_id, kind, name, ciphertext, nonce, created_at, updated_at)
		VALUES ('s1', 1, 'work', 'login', 'bank-login', x'53454352455421', x'00', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}

	cfg := &globalConfig{DBPath: dbPath, ProfileID: "work", JSONOutput: true}
	out := captureStdout(t, func() {
		cmd := newProfileCmd(cfg)
		cmd.SetArgs([]string{"context"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{`"name": "Work Stuff"`, custom, "bank-login"} {
		if !strings.Contains(out, want) {
			t.Errorf("profile context lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SECRET") {
		t.Errorf("secret leaked:\n%s", out)
	}

	prompt := coderLeadSystemPrompt(context.Background(), db.DB, cfg, custom)
	if !strings.Contains(prompt, `"Work Stuff"`) || !strings.Contains(prompt, "bank-login") || !strings.Contains(prompt, "working in "+custom) {
		t.Errorf("lead prompt does not name the profile:\n%s", prompt)
	}
	other := coderLeadSystemPrompt(context.Background(), db.DB, &globalConfig{ProfileID: "default"}, custom)
	if strings.Contains(other, "Work Stuff") || strings.Contains(other, "bank-login") {
		t.Errorf("default profile's prompt leaks the other:\n%s", other)
	}
}
