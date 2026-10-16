package monomind

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An org run's role shells get the vouched org kind; the foreground run
// gives its record back when it ends.
func TestOrgRunVouchesForTheOrgKind(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "env")
	root := fakeOrgMonomind(t, `echo "$MONOAGENT_AGENT_KIND $MONOAGENT_AGENT_KIND_TOKEN" > `+seen+"\necho '{}'")
	done, bound := 0, 0
	OrgKindEnv = func() ([]string, func(int), func()) {
		return []string{"MONOAGENT_AGENT_KIND=org", "MONOAGENT_AGENT_KIND_TOKEN=tok-1"}, func(int) { bound++ }, func() { done++ }
	}
	t.Cleanup(func() { OrgKindEnv = nil })
	if _, err := OrgRun(context.Background(), root, "sec", "", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(seen); strings.TrimSpace(string(got)) != "org tok-1" || done != 1 || bound != 0 {
		t.Errorf("the org saw %q, done %d times, bound %d times", got, done, bound)
	}

	OrgKindEnv = nil
	cmd := Command("true")
	before := len(cmd.Env)
	bind, kindDone := withOrgKind(cmd)
	bind(1)
	if kindDone(); len(cmd.Env) != before {
		t.Error("no hook, no env")
	}
}

// A detached launch (org run --start, org serve) binds the kind to the
// process it started, which outlives the launcher, and never gives the
// record back.
func TestDetachedOrgLaunchesBindTheKindToTheirProcess(t *testing.T) {
	setWatch(t, 20*time.Second)
	root := t.TempDir()
	rt := filepath.Join(root, ".monomind", "orgs", "sec", "runtime.json")
	if err := os.MkdirAll(filepath.Dir(rt), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeOrgMonomind(t, `[ "$2" = serve ] && exec sleep 30`+"\n"+
		`printf '{"status":"running","run":"r1","pid":%s,"updated":"%s"}' $$ "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)" > '`+rt+"'\nexec sleep 30")
	var bound []int
	done := 0
	OrgKindEnv = func() ([]string, func(int), func()) {
		return []string{"MONOAGENT_AGENT_KIND=org"}, func(pid int) { bound = append(bound, pid) }, func() { done++ }
	}
	t.Cleanup(func() { OrgKindEnv = nil })
	kill := func(pid int) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	}

	if err := OrgRunStart(context.Background(), root, "sec", ""); err != nil {
		t.Fatal(err)
	}
	var run struct {
		PID int `json:"pid"`
	}
	raw, _ := os.ReadFile(rt)
	if err := json.Unmarshal(raw, &run); err != nil || run.PID <= 0 {
		t.Fatalf("runtime.json %q: %v", raw, err)
	}
	t.Cleanup(func() { kill(run.PID) })
	if len(bound) != 1 || bound[0] != run.PID || done != 0 {
		t.Fatalf("org run --start bound %v (the run is %d), done %d times", bound, run.PID, done)
	}

	pid, already, err := OrgServeStart(context.Background(), root)
	if err != nil || already {
		t.Fatalf("serve start: %v, already %v", err, already)
	}
	t.Cleanup(func() { kill(pid) })
	if len(bound) != 2 || bound[1] != pid || done != 0 {
		t.Errorf("org serve bound %v (the daemon is %d), done %d times", bound, pid, done)
	}
}
