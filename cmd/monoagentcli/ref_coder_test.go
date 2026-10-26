package main

import (
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// The `ref commands` entries for the coder host and sessions are held to the commands as
// built: a flag that is renamed or a subcommand that is invented fails here, not in an
// agent that follows the text.

func coderDocs() map[string]cmdDoc {
	out := map[string]cmdDoc{}
	for _, d := range cliDocs {
		if strings.HasPrefix(d.Name, "coder host") || strings.HasPrefix(d.Name, "coder sessions") {
			out[d.Name] = d
		}
	}
	return out
}

func TestEveryCoderHostAndSessionsCommandHasAReferenceEntry(t *testing.T) {
	docs := coderDocs()
	root := newRootCmd()
	for _, group := range [][]string{{"coder", "host"}, {"coder", "sessions"}, {"coder", "sessions", "settings"}} {
		g, _, err := root.Find(group)
		if err != nil || g == nil || g.Name() != group[len(group)-1] {
			t.Fatalf("no command %v", group)
		}
		for _, sub := range g.Commands() {
			if sub.Name() == "help" {
				continue
			}
			name := strings.Join(group, " ") + " " + sub.Name()
			if _, ok := docs[name]; !ok {
				t.Errorf("`ref commands` has no entry for `%s`", name)
			}
		}
		if _, ok := docs[strings.Join(group, " ")]; !ok {
			t.Errorf("`ref commands` has no entry for `%s`", strings.Join(group, " "))
		}
	}
}

func TestCoderReferenceEntriesNameOnlyRealCommandsAndFlags(t *testing.T) {
	root := newRootCmd()
	for name, d := range coderDocs() {
		cmd, _, err := root.Find(strings.Fields(name))
		if err != nil || cmd == nil || strings.Join(strings.Fields(cmd.CommandPath())[1:], " ") != name {
			t.Errorf("`ref commands` documents `%s`, which is not a command", name)
			continue
		}
		for _, f := range refFlagsIn(d.Flags) {
			if cmd.Flags().Lookup(f) == nil && cmd.InheritedFlags().Lookup(f) == nil {
				t.Errorf("`%s` documents --%s, which the command does not have", name, f)
			}
		}
		for _, ex := range d.Examples {
			for _, f := range refFlagsIn(ex) {
				if root.PersistentFlags().Lookup(f) == nil && cmd.Flags().Lookup(f) == nil && cmd.InheritedFlags().Lookup(f) == nil {
					t.Errorf("an example of `%s` uses --%s, which does not exist: %s", name, f, ex)
				}
			}
		}
		// every flag the real command has (other than help) is documented
		cmd.LocalFlags().VisitAll(func(fl *pflag.Flag) {
			if fl.Name != "help" && !strings.Contains(d.Flags, "--"+fl.Name) {
				t.Errorf("`%s` has --%s, which its `ref commands` entry does not document", name, fl.Name)
			}
		})
	}
}
