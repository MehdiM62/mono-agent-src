package main

import (
	"strings"
	"testing"
)

// An empty --profile value names no profile: the gate refuses a scoped command
// that got an empty value, and a global command is unaffected.
func TestEmptyProfileFlagIsNotGiven(t *testing.T) {
	for _, args := range [][]string{
		{"--profile", "", "people", "list"},
		{"--profile=", "people", "list"},
	} {
		_, err := runGateCLI(t, args...)
		if err == nil || exitCodeFor(err) != 3 || !strings.Contains(err.Error(), "--profile") {
			t.Fatalf("%v: err %v (code %d), want exit 3 naming --profile", args, err, exitCodeFor(err))
		}
	}
	for _, args := range [][]string{{"--profile", "", "config", "list"}, {"--profile=", "version"}} {
		out, err := runGateCLI(t, args...)
		if err != nil || strings.Contains(out, "acts on a profile") {
			t.Fatalf("%v: got %v / %q", args, err, out)
		}
	}
}
