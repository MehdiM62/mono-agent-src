package main

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/cli/profilegate"
	"github.com/monoes/mono-agent/internal/cli/ref/catalog"
	"github.com/spf13/cobra"
)

// scopedWithoutProfile reports the tree command a `monoagentcli ...` example
// line runs when that command is scoped and the line does not name --profile.
// A line that is no example (a comment, no binary name, no command of the
// tree) reports false, as does one that is global or has --profile.
func scopedWithoutProfile(t *testing.T, root *cobra.Command, line string) (resolved, bad bool) {
	t.Helper()
	i := strings.Index(line, "monoagentcli ")
	if i < 0 || strings.HasPrefix(strings.TrimSpace(line), "#") {
		return false, false
	}
	var words []string
	hasProfile := false
	toks := strings.Fields(line[i+len("monoagentcli "):])
scan:
	for j := 0; j < len(toks); j++ {
		tok := toks[j]
		switch {
		case tok == "--help" || tok == "-h":
			return false, false // help runs before the gate
		case tok == "--profile":
			hasProfile = true
			j++
		case strings.HasPrefix(tok, "--profile="):
			hasProfile = true
		case strings.HasPrefix(tok, "-"):
			// another global flag (--json), or the command's own flags
			if len(words) > 0 {
				break scan
			}
		case strings.ContainsAny(tok, "<[|`'\"\\") || tok == "#":
			break scan
		default:
			words = append(words, tok)
		}
	}
	// The longest prefix of the words that is a command of the tree.
	for n := len(words); n > 0; n-- {
		if c, rest, err := root.Find(words[:n]); err == nil && c != root && len(rest) == 0 {
			return true, !hasProfile && profilegate.Class(c) == profilegate.ClassScoped
		}
	}
	return false, false
}

// Every `ref commands` example of a scoped command names its profile, so the
// examples an agent copies keep working once the gate enforces.
func TestRefCatalogExamplesNameTheirProfile(t *testing.T) {
	root := newRootCmd()
	checked := 0
	for _, doc := range catalog.CommandDocs {
		for _, ex := range doc.Examples {
			resolved, bad := scopedWithoutProfile(t, root, ex)
			if resolved {
				checked++
			}
			if bad {
				t.Errorf("%s: scoped example has no --profile: %s", doc.Name, ex)
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d examples resolved to a command; the walk is broken", checked)
	}
}

// The same holds for the command lines the `ref <topic>` pages print.
func TestRefTopicExamplesNameTheirProfile(t *testing.T) {
	topics := []string{"templates", "workflow", "connections", "expressions", "examples",
		"crawling", "api", "org", "publication", "tasks", "account"}
	checked := 0
	for _, topic := range topics {
		root := newRootCmd()
		root.SetArgs([]string{"ref", topic})
		var execErr error
		out := captureStdout(t, func() { execErr = root.ExecuteContext(context.Background()) })
		if execErr != nil {
			t.Fatalf("ref %s: %v", topic, execErr)
		}
		for _, line := range strings.Split(out, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "monoagentcli ") {
				continue
			}
			resolved, bad := scopedWithoutProfile(t, newRootCmd(), line)
			if resolved {
				checked++
			}
			if bad {
				t.Errorf("ref %s: scoped example has no --profile: %s", topic, strings.TrimSpace(line))
			}
		}
	}
	if checked < 30 {
		t.Fatalf("only %d topic lines resolved to a command; the walk is broken", checked)
	}
}
