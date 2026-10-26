package chat

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// --mission confirms a mission on the user's behalf: under agent-context markers it is refused,
// as `coder set`'s operator flags are; the desktop app (no markers) still passes it.
func TestChatMissionFlagIsRefusedToAnAgent(t *testing.T) {
	agent := testDeps(&testConfig{DBPath: t.TempDir() + "/x.db", ProfileID: "default"})
	agent.RequireOperator = func(what string) error { return clicore.OperatorOnly("only the user can %s", what) }
	cmd := newChatCmd(agent)
	cmd.SetArgs([]string{"--mission", "--runtime", "fake", "hi"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); exitCodeFor(err) != 3 || err == nil || !strings.Contains(err.Error(), "only the user can confirm a mission") {
		t.Fatalf("an agent passed --mission: exit %d (%v)", exitCodeFor(err), err)
	}

	user := testDeps(&testConfig{DBPath: t.TempDir() + "/y.db", ProfileID: "default"})
	user.RequireOperator = func(string) error { return nil }
	cmd = newChatCmd(user)
	cmd.SetArgs([]string{"--mission", "--runtime", "fake", "hi"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--mission only applies to a coder conversation") {
		t.Fatalf("the operator's --mission got past the gate wrongly: %v", err)
	}
}
