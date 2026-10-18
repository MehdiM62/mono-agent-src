package main

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// The help of each command an agent, the chat and a workflow are refused
// says so, as the reveal flags do (#845 code review).
func TestTheUsersOwnCommandsSaySoInTheirHelp(t *testing.T) {
	root := newRootCmd()
	for _, path := range [][]string{
		{"api", "config", "set"}, {"api", "config", "unset"},
		{"api", "key", "create"}, {"api", "key", "update"}, {"api", "key", "revoke"},
		{"jev", "enable"}, {"jev", "key", "set"}, {"jev", "key", "adopt"}, {"jev", "key", "remove"},
		{"org", "teardown-profile"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] {
			t.Fatalf("%v: %v", path, err)
		}
		if !strings.Contains(cmd.Long, clicore.OperatorOnlyHelp) {
			t.Errorf("the help of %s doesn't say it is the user's own:\n%s", strings.Join(path, " "), cmd.Long)
		}
	}
}
