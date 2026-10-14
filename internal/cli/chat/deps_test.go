package chat

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The commands main registers, with their subcommands and the flags the desktop app and the
// browser extension pass.
func TestCommandsTree(t *testing.T) {
	t.Parallel()
	d := *testDeps(&testConfig{DBPath: filepath.Join(t.TempDir(), "x.db")})
	names := func(subs []string) string { sort.Strings(subs); return strings.Join(subs, " ") }
	chat := NewChatCommand(d)
	var subs []string
	for _, c := range chat.Commands() {
		subs = append(subs, c.Name())
	}
	if chat.Name() != "chat" || len(subs) == 0 {
		t.Fatalf("chat = %q, subcommands %q", chat.Name(), names(subs))
	}
	for _, f := range []string{"runtime", "conversation", "turn"} {
		if chat.Flags().Lookup(f) == nil {
			t.Errorf("chat lacks --%s", f)
		}
	}
	coder := NewCoderCommand(d)
	if coder.Name() != "coder" || !coder.HasSubCommands() {
		t.Fatalf("coder = %q", coder.Name())
	}
	if set := NewCoderSetCommand(d); set.Name() != "set" || set.Flags().Lookup("send-approval") == nil {
		t.Errorf("coder set = %q", set.Name())
	}
}

// The chat control files live next to the database Deps.DBPath names.
func TestControlDirFollowsDBPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	got := agentControlDir(testDeps(&testConfig{DBPath: filepath.Join(dir, "m.db")}), "turn-9")
	if !strings.HasPrefix(got, filepath.Join(dir, "chat-control")) {
		t.Errorf("control dir = %q", got)
	}
}
