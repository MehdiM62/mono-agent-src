package main

import (
	"strings"
	"testing"
)

func TestUnknownSubcommandListsAvailable(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"org", "show", "x"})
	root.SilenceUsage = true
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "available:") || !strings.Contains(err.Error(), "logs") {
		t.Fatalf("want error listing subcommands, got %v", err)
	}
}

func TestUnknownCommandSuggestsClosestName(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"wrkflow", "list"}, `Did you mean: workflow?`},
		{[]string{"workflow", "lsit"}, `Did you mean: list?`},
	} {
		root := newRootCmd()
		root.SetArgs(tc.args)
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "available:") {
			t.Errorf("%v: want %q and the available list, got %v", tc.args, tc.want, err)
		}
	}
}

func TestUnknownCommandWithoutCloseNameHasNoSuggestion(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"zzzzzzzz"})
	err := root.Execute()
	if err == nil || strings.Contains(err.Error(), "Did you mean") || !strings.Contains(err.Error(), "available:") {
		t.Fatalf("want the available list and no suggestion, got %v", err)
	}
}
