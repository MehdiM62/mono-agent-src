package main

import (
	"sort"
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
