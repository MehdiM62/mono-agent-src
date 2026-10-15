package org

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With a database that cannot be opened an org command has no profile folder
// to act on. It must fail, and must not fall back to the current directory
// (filepath.Join("", ...) is relative to it).
func TestOrgCommandsWithUnopenableDBTouchNothingUnderCwd(t *testing.T) {
	groups := map[string][][]string{
		"proxy": {
			{"list"}, {"status", "acme"}, {"logs", "acme"}, {"report", "acme"}, {"costs", "acme"},
			{"flow", "acme"}, {"questions", "acme"}, {"approvals", "acme"}, {"gates", "acme"},
			{"decisions", "acme"}, {"memory", "acme"}, {"events", "acme"}, {"reload", "acme"},
			{"validate", "acme"}, {"answer", "acme", "q1", "yes"}, {"approve", "acme", "ceo", "ship"},
		},
		"root": {
			{"documents", "acme"}, {"schedule-audit", "acme"}, {"queued", "acme"},
			{"sign", "acme"}, {"serve"}, {"serve", "--stop"}, {"stop", "acme"}, {"pause", "acme"},
			{"resume", "acme"}, {"summary"}, {"legacy", "list"},
			{"chat", "history", "acme"}, {"chat", "approve", "acme", "gate-1"}, {"chat", "dismiss", "acme", "q1"},
		},
		"profile": {
			{"send", "acme", "--to", "ceo", "--body", "hi"}, {"rename", "acme", "beta"},
			{"delete", "acme", "--force"}, {"autonomy", "show", "acme"},
		},
	}
	for group, cmds := range groups {
		for _, args := range cmds {
			name := group + "/" + strings.Join(args, " ")
			t.Run(name, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("MONOMIND_BIN", "/nonexistent/monomind")
				cwd := t.TempDir()
				t.Chdir(cwd)
				// the database path is a directory: it cannot be opened
				cfg := &testConfig{DBPath: t.TempDir(), ProfileID: "default"}
				cmd := newOrgCmd(cfg)
				cmd.SetArgs(args)
				cmd.SilenceUsage, cmd.SilenceErrors = true, true
				var err error
				captureStdout(t, func() { err = cmd.Execute() })
				if err == nil {
					t.Fatalf("org %v succeeded with no database", args)
				}
				if strings.Contains(err.Error(), "accepts") || strings.Contains(err.Error(), "unknown") {
					t.Fatalf("org %v failed on its arguments, not the database: %v", args, err)
				}
				entries, _ := os.ReadDir(cwd)
				if len(entries) != 0 {
					var names []string
					for _, e := range entries {
						names = append(names, filepath.Join(cwd, e.Name()))
					}
					t.Fatalf("org %v wrote under the current directory: %v", args, names)
				}
			})
		}
	}
}
