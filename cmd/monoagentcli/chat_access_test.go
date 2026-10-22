package main

import (
	"errors"
	"testing"

	"github.com/spf13/pflag"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/storage"
)

func accessOf(t *testing.T, dbPath, id string) string {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := ai.NewAIStore(db.DB)
	if err != nil {
		t.Fatal(err)
	}
	c, err := store.GetConversation(id, "default")
	if err != nil {
		t.Fatal(err)
	}
	return c.EffectiveAccess()
}

func seedConversation(t *testing.T, dbPath, access string) string {
	t.Helper()
	db, err := storage.NewDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := ai.NewAIStore(db.DB)
	if err != nil {
		t.Fatal(err)
	}
	cwd := ""
	if access != ai.AccessScoped {
		cwd = t.TempDir()
	}
	c, err := store.CreateConversationAccess("default", "agent", "general", "claude", "", "", access, cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

// Raising a conversation's access is the operator's: an agent, the chat and a
// workflow are refused (exit 3, operator_only) with every flag set-access has,
// and the level stays. A new flag fails here until it gets a value, which is
// the moment to decide whether only the operator may use it. Lowering is
// anyone's: the same agent lowers it.
func TestChatSetAccessRaiseIsTheOperators(t *testing.T) {
	cfg := sendEnv(t)
	conv := seedConversation(t, cfg.DBPath, ai.AccessScoped)
	set := func(args ...string) error {
		_, err := runSendCLI(t, cfg, newChatCmd(cfg), append([]string{"history", "set-access", conv}, args...)...)
		return err
	}
	values := map[string][]string{
		"cwd": {"--cwd=" + t.TempDir()}, "profile-folder": {"--profile-folder"},
		"coder-root": {"--coder-root"}, "new-workspace": {"--new-workspace"},
	}
	cmd := newChatCmd(cfg)
	sub, _, err := cmd.Find([]string{"history", "set-access"})
	if err != nil {
		t.Fatal(err)
	}
	sub.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" || f.Name == "json" {
			return
		}
		if _, ok := values[f.Name]; !ok {
			t.Errorf("--%s has no test value: decide whether only the operator may use it, then add it here", f.Name)
		}
	})
	asAgentProcess(t)
	attempts := [][]string{{"coding"}, {"full"}}
	for _, flag := range values {
		attempts = append(attempts, append([]string{"full"}, flag...))
	}
	for _, args := range attempts {
		err := set(args...)
		var coded interface{ JSONErrorFields() map[string]any }
		if clicore.ExitCodeFor(err) != 3 || !errors.As(err, &coded) || coded.JSONErrorFields()["code"] != "operator_only" {
			t.Errorf("an agent ran set-access %v: %v", args, err)
		}
	}
	if got := accessOf(t, cfg.DBPath, conv); got != ai.AccessScoped {
		t.Fatalf("an agent's refused raise left the level at %q", got)
	}
	full := seedConversation(t, cfg.DBPath, ai.AccessFull)
	if _, err := runSendCLI(t, cfg, newChatCmd(cfg), "history", "set-access", full, "scoped"); err != nil {
		t.Fatalf("an agent lowered a conversation: %v", err)
	}
	if got := accessOf(t, cfg.DBPath, full); got != ai.AccessScoped {
		t.Errorf("the agent's lowering left the level at %q", got)
	}
}
