package hil

import (
	"strings"
	"testing"
)

// A Deps without its operator check refuses what only the user decides: it
// fails closed rather than panicking or letting it through (#845 follow-up).
func TestHILWithoutAnOperatorCheckRefuses(t *testing.T) {
	cfg := &testConfig{DBPath: newHILTestDB(t), ProfileID: "default", JSONOutput: true}
	d := testDeps(cfg)
	d.RequireOperator = nil
	cmd := NewCommand(d)
	cmd.SetArgs([]string{"allow", "add", "--action", "email_send", "--recipient", "ada@example.test"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	var err error
	captureStdout(t, func() { err = cmd.Execute() })
	if err == nil || !strings.Contains(err.Error(), "only the user can allow a destination: the operator check is not wired") {
		t.Fatalf("allow add without an operator check: %v", err)
	}
}
