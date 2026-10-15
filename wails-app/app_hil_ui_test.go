package main

import (
	"context"
	"strings"
	"testing"
)

// The bubble ask bindings only shell out to `monoagentcli hil …`, with the
// values after "--".
func TestHILUIBindingsShellOutToCLI(t *testing.T) {
	log := fakeMonoCLI(t, `[{"id":"s1","kind":"send","state":"pending"}]`)
	a := newTestApp(t)
	a.ctx = context.Background()
	a.selectForTest("work")

	if got := a.HILList(); !strings.Contains(got, `"kind":"send"`) {
		t.Fatalf("HILList = %s, want the CLI's rows untouched", got)
	}
	if got := a.HILResolve("s1", true, "edited text", ""); !strings.Contains(got, `"ok":true`) {
		t.Fatalf("HILResolve approve = %s", got)
	}
	if got := a.HILResolve("s1", false, "", ""); !strings.Contains(got, `"rejected"`) {
		t.Fatalf("HILResolve reject = %s", got)
	}
	if got := a.HILResolve("w1", true, "", `{"caption":"y"}`); !strings.Contains(got, `"approved"`) {
		t.Fatalf("HILResolve workflow = %s", got)
	}
	if got := a.HILResolve("w1", true, "", "{nope"); !strings.Contains(got, "error") {
		t.Fatalf("invalid data must be refused before the CLI runs: %s", got)
	}
	a.HILAttachment("s1", 2)
	want := []string{
		"--profile work --json hil list",
		`--profile work --json hil approve --data {"text":"edited text"} -- s1`,
		"--profile work --json hil reject -- s1",
		`--profile work --json hil approve --data {"caption":"y"} -- w1`,
		"--profile work --json hil attachment -- s1 2",
	}
	if got := loggedArgs(t, log); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("CLI calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
