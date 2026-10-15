package profilegate

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func twoProfiles() ([]ProfileRef, error) {
	return []ProfileRef{{ID: "a1", Name: "Work"}, {ID: "b2", Name: "Home"}}, nil
}

func TestScopedWithoutProfileIsRefusedWhenEnforcing(t *testing.T) {
	_, err := Check(find(tree(), "people", "list"), false, ModeEnforce, twoProfiles)
	if err == nil || !strings.Contains(err.Error(), "--profile") || !strings.Contains(err.Error(), "Work") {
		t.Fatalf("want an error naming --profile and the available profiles, got %v", err)
	}
}

func TestScopedWithoutProfileWarnsInWarnMode(t *testing.T) {
	w, err := Check(find(tree(), "people", "list"), false, ModeWarn, twoProfiles)
	if err != nil || !strings.Contains(w, "--profile") || !strings.Contains(w, "Home (b2)") {
		t.Fatalf("want a warning and no error, got %q / %v", w, err)
	}
}

func TestTheProfileListIsBestEffort(t *testing.T) {
	failing := func() ([]ProfileRef, error) { return nil, errors.New("no database") }
	for _, list := range []Lister{nil, failing} {
		w, err := Check(find(tree(), "people", "list"), false, ModeWarn, list)
		if err != nil || !strings.Contains(w, "--profile") || strings.Contains(w, "Profiles:") {
			t.Fatalf("want the warning without a list, got %q / %v", w, err)
		}
	}
}

func TestGlobalNeverNeedsAProfile(t *testing.T) {
	w, err := Check(find(tree(), "config", "get"), false, ModeEnforce, nil)
	if err != nil || w != "" {
		t.Fatalf("global command must pass silently, got %q / %v", w, err)
	}
}

func TestScopedWithProfilePasses(t *testing.T) {
	w, err := Check(find(tree(), "people", "list"), true, ModeEnforce, nil)
	if err != nil || w != "" {
		t.Fatalf("got %q / %v", w, err)
	}
}

func TestHelpCompletionAndTheRootNeverNeedAProfile(t *testing.T) {
	root := tree()
	var completion *cobra.Command
	for _, name := range []string{"help", "completion", "__complete", "__completeNoDesc"} {
		c := &cobra.Command{Use: name}
		root.AddCommand(c)
		if name == "completion" {
			completion = c
		}
		if w, err := Check(c, false, ModeEnforce, nil); err != nil || w != "" {
			t.Fatalf("%s: got %q / %v", name, w, err)
		}
	}
	if w, err := Check(root, false, ModeEnforce, nil); err != nil || w != "" {
		t.Fatalf("root: got %q / %v", w, err)
	}
	sub := &cobra.Command{Use: "bash"}
	completion.AddCommand(sub)
	if w, err := Check(sub, false, ModeEnforce, nil); err != nil || w != "" {
		t.Fatalf("completion bash: got %q / %v", w, err)
	}
}
