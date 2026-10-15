package main

import (
	"strings"
	"testing"

	refcli "github.com/monoes/mono-agent/internal/cli/ref"
)

// Every `browser` command and flag `ref browser` (and the agents' prompts)
// name exists on the real CLI.
func TestRefBrowserNamesRealCommands(t *testing.T) {
	root := newRootCmd()
	browser, _, err := root.Find([]string{"browser"})
	if err != nil || browser == root {
		t.Fatal("no `browser` command")
	}
	for _, f := range []string{"browser", "tab"} {
		if browser.PersistentFlags().Lookup(f) == nil {
			t.Errorf("`browser` has no --%s", f)
		}
	}
	check := func(path string, flags []string, required bool) {
		c, _, err := root.Find(strings.Fields("browser " + path))
		if err != nil || c == browser || c.Name() != strings.Fields(path)[len(strings.Fields(path))-1] {
			if required {
				t.Errorf("`ref browser` names `browser %s`, which does not exist", path)
			} else {
				t.Logf("`browser %s` is not registered in this build (visual-styles adds it)", path)
			}
			return
		}
		for _, f := range flags {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("`ref browser` names --%s of `browser %s`, which it does not have", f, path)
			}
		}
		if !strings.Contains(refcli.BrowserText, "browser "+path) {
			t.Errorf("`ref browser` does not mention `browser %s`", path)
		}
	}
	for path, flags := range map[string][]string{
		"browsers": nil, "tabs": nil, "open": {"group"}, "navigate": nil, "reload": {"hard"}, "close": nil,
		"click": nil, "type": nil, "press": nil, "wait": {"timeout"}, "eval": nil,
		"screenshot": {"full", "element", "width", "height", "media", "out"}, "a11y": {"selector"},
		"console": {"since", "level"}, "network": {"since", "failed"},
		"pick": {"prompt", "timeout", "out"}, "allow list": nil, "allow add": nil, "allow remove": nil, "submit": {"hil"},
	} {
		check(path, flags, true)
	}
	// Added by the visual-styles files through browserSubcommands.
	for path, flags := range map[string][]string{
		"styles": {"out"}, "tokens": {"out"}, "source": nil, "diff": {"out", "threshold"}, "sweep": {"widths", "media", "out"},
	} {
		check(path, flags, false)
	}
}
