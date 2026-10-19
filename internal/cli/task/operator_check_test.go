package task

import (
	"strings"
	"testing"
)

// A Deps without its operator check refuses what only the operator decides,
// as one that says the caller isn't the operator does: an unwired check
// fails closed (#845 follow-up).
func TestTaskWithoutAnOperatorCheckDecidesNothing(t *testing.T) {
	t.Parallel()
	db := newTaskTestDB(t)
	inbox := opsAdd(t, db, "waiting")
	d := testDeps(t, db, "default", true)
	d.NotOperator = nil
	cmd := NewCommand(d)
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"approve", id(inbox)})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "operator check is not wired") {
		t.Fatalf("task approve without an operator check: %v", err)
	}
}
