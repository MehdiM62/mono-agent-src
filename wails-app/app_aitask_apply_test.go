//go:build !windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestAITaskApplyBindingsRunTheExactCommands(t *testing.T) {
	a, log := aitaskApp(t, `echo '{"ok":true,"workflowId":"w1"}'`+"\n")
	for _, call := range []func(string) (string, error){a.PreviewFailedRunFix, a.ApplyFailedRunFix, a.SaveGeneratedWorkflow} {
		got, err := call("task-1")
		if err != nil || got != `{"ok":true,"workflowId":"w1"}` {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	want := "--profile work --json workflow apply-fix --dry-run -- task-1\n--profile work --json workflow apply-fix -- task-1\n--profile work --json workflow save-draft -- task-1"
	if calls := loggedArgs(t, log); strings.Join(calls, "\n") != want {
		t.Fatalf("CLI calls = %v", calls)
	}
}

func TestAITaskApplyBindingsKeepTheCLIsRefusal(t *testing.T) {
	a, _ := aitaskApp(t, `echo '{"error":"the workflow changed since the fix was proposed","code":"stale"}'; exit 1`+"\n")
	_, err := a.ApplyFailedRunFix("task-1")
	if err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("err = %v", err)
	}
}

func TestAITaskApplyBindingsRefuseAHostileIDBeforeTheCLI(t *testing.T) {
	a, log := aitaskApp(t, "echo '{}'\n")
	for _, id := range []string{"", "--all", "-x", "a b", "a;rm", "../x", strings.Repeat("a", 65)} {
		if _, err := a.ApplyFailedRunFix(id); err == nil {
			t.Errorf("id %q was accepted", id)
		}
		if _, err := a.PreviewFailedRunFix(id); err == nil {
			t.Errorf("preview id %q was accepted", id)
		}
		if _, err := a.SaveGeneratedWorkflow(id); err == nil {
			t.Errorf("save id %q was accepted", id)
		}
	}
	if _, err := os.Stat(log); err == nil {
		t.Fatalf("CLI was run: %v", loggedArgs(t, log))
	}
}
