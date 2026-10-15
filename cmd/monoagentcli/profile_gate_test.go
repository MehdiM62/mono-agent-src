package main

import (
	"bytes"
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/cli/profilegate"
	"github.com/spf13/cobra"
)

// Leaf counts of the real command tree. They change when a command is added,
// removed or reclassified: update them deliberately, after deciding the
// command's class in internal/cli/profilegate/classes.go.
const (
	wantLeafCommands = 417
	wantGlobalLeaves = 106
)

// cobraOwned are table entries for commands cobra adds while a command runs,
// so they are not in the tree this test builds.
var cobraOwned = map[string]bool{"help": true, "completion": true}

func leafCommands(root *cobra.Command) (all, global []string) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, s := range c.Commands() {
			if len(s.Commands()) == 0 {
				all = append(all, profilegate.CommandKey(s))
				if profilegate.Class(s) == profilegate.ClassGlobal {
					global = append(global, profilegate.CommandKey(s))
				}
				continue
			}
			walk(s)
		}
	}
	walk(root)
	sort.Strings(all)
	sort.Strings(global)
	return all, global
}

func TestProfileGateTable(t *testing.T) {
	root := newRootCmd()
	for key := range profilegate.Listed() {
		if cobraOwned[key] {
			continue
		}
		fields := splitKey(key)
		if c, rest, err := root.Find(fields); err != nil || c == root || len(rest) != 0 {
			t.Errorf("profilegate lists %q, which is no command of the tree", key)
		}
	}
	all, global := leafCommands(root)
	if len(all) != wantLeafCommands || len(global) != wantGlobalLeaves {
		t.Errorf("leaf commands = %d (want %d), global = %d (want %d); global leaves:\n%v",
			len(all), wantLeafCommands, len(global), wantGlobalLeaves, global)
	}
}

// runGateCLI executes args on a fresh root over a scratch database and returns
// the command's error and stderr. It skips the account gate by design.
func runGateCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append([]string{"--db-path", filepath.Join(t.TempDir(), "gate.db")}, args...))
	err := root.ExecuteContext(context.Background())
	return errb.String(), err
}

func TestScopedCommandWithoutProfileWarnsAndRuns(t *testing.T) {
	stderr, err := runGateCLI(t, "people", "list")
	if err != nil {
		t.Fatalf("people list must still run in warn mode: %v", err)
	}
	if !strings.Contains(stderr, "--profile") || !strings.Contains(stderr, "falling back to the active profile") {
		t.Fatalf("want the profile warning on stderr, got %q", stderr)
	}
}

func TestScopedCommandWithProfileIsSilent(t *testing.T) {
	stderr, err := runGateCLI(t, "--profile", "default", "people", "list")
	if err != nil || strings.Contains(stderr, "acts on a profile") {
		t.Fatalf("got %v / %q", err, stderr)
	}
}

func TestGlobalCommandsAndHelpAreSilent(t *testing.T) {
	for _, args := range [][]string{{"config", "list"}, {"version"}, {"help"}, {"help", "people"}} {
		stderr, err := runGateCLI(t, args...)
		if err != nil || strings.Contains(stderr, "acts on a profile") {
			t.Fatalf("%v: got %v / %q", args, err, stderr)
		}
	}
}

func TestACommandWithItsOwnPreRunStillGetsTheGate(t *testing.T) {
	root := newRootCmd()
	doctor, _, err := root.Find([]string{"doctor"})
	if err != nil || doctor == nil || doctor.PersistentPreRun == nil {
		t.Fatalf("doctor has no pre-run of its own: %v", err)
	}
	var errb bytes.Buffer
	doctor.SetErr(&errb)
	doctor.SetContext(context.Background())
	_ = doctor.ParseFlags(nil)
	doctor.PersistentPreRun(doctor, nil)
	if !strings.Contains(errb.String(), "--profile") {
		t.Fatalf("want the warning from doctor's own pre-run, got %q", errb.String())
	}
}

func splitKey(key string) []string {
	var out []string
	cur := ""
	for _, r := range key {
		if r == ' ' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
