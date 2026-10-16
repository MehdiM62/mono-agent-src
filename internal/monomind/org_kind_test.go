package monomind

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An org run's role shells get the vouched org kind; the foreground run
// gives its record back when it ends.
func TestOrgRunVouchesForTheOrgKind(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "env")
	root := fakeOrgMonomind(t, `echo "$MONOAGENT_AGENT_KIND $MONOAGENT_AGENT_KIND_TOKEN" > `+seen+"\necho '{}'")
	done := 0
	OrgKindEnv = func() ([]string, func()) {
		return []string{"MONOAGENT_AGENT_KIND=org", "MONOAGENT_AGENT_KIND_TOKEN=tok-1"}, func() { done++ }
	}
	t.Cleanup(func() { OrgKindEnv = nil })
	if _, err := OrgRun(context.Background(), root, "sec", "", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(seen); strings.TrimSpace(string(got)) != "org tok-1" || done != 1 {
		t.Errorf("the org saw %q, done %d times", got, done)
	}

	OrgKindEnv = nil
	cmd := Command("true")
	before := len(cmd.Env)
	if withOrgKind(cmd)(); len(cmd.Env) != before {
		t.Error("no hook, no env")
	}
}
