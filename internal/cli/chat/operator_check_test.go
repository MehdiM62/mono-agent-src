package chat

import (
	"strings"
	"testing"
)

// A Deps without its operator check refuses what only the user decides: it
// fails closed rather than panicking or letting it through (#845 follow-up).
func TestCoderWithoutAnOperatorCheckRefuses(t *testing.T) {
	withCoderCaps(t)
	d := testDeps(&testConfig{DBPath: newChatCLITestDB(t), ProfileID: "default", JSONOutput: true})
	d.RequireOperator = nil
	cmd := newCoderCmd(d)
	cmd.SetOut(&strings.Builder{})
	cmd.SetArgs([]string{"enable", "--yes-i-understand"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	err := cmd.Execute()
	if exitCodeFor(err) != 3 || !strings.Contains(err.Error(), "operator check is not wired") {
		t.Fatalf("coder enable without an operator check: %v", err)
	}
}
