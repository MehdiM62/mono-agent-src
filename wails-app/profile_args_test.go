package main

import "testing"

func namesProfile(argv []string, id string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--profile" && argv[i+1] == id {
			return true
		}
	}
	return false
}

// Every scoped command the app issues must name its profile, so the CLI's
// profile gate has nothing to warn about.
func TestScopedCLIArgsNameTheProfile(t *testing.T) {
	const p = "prof-1"
	cases := map[string][]string{
		"workflow list":    scopedCLIArgs(p, "workflow", "list"),
		"task":             scopedCLIArgs(p, "task", "list"),
		"secret":           scopedCLIArgs(p, "secret", "list"),
		"summary":          scopedCLIArgs(p, "summary"),
		"org list":         orgCLIRunArgs(p, "", "list"),
		"org with project": orgCLIRunArgs(p, "/root", "status", "growth"),
		"org events":       orgCLIRunArgs(p, "/root", "events", "--follow", "--", "growth"),
		"org run":          orgCLIRunArgs(p, "", "run", "growth", "--task", "x"),
		"org validate":     orgCLIRunArgs(p, "/root", "validate", "growth"),
		"org json args":    orgCLIArgs(p, "/root", []string{"list"}),
		"status":           statusCLIArgs(p),
		"automation":       automationCLIArgs(p, "automation", "list"),
		"workflow import":  workflowImportArgs(p, "f.json", WorkflowImportOptions{}),
		"workflow delete":  workflowDeleteArgs(p, "wf1"),
		"chat turn":        chatTurnArgs(p, "c", "t", "i", "hi", false, false),
		"doctor":           mustHealthArgs(t, p, healthModeLocal),
		"doctor fix":       healthFixArgs(p, "fix-id"),
	}
	for name, argv := range cases {
		if !namesProfile(argv, p) {
			t.Errorf("%s: %v does not pass --profile %s", name, argv, p)
		}
	}
}

// Global commands (account, agent, db, update, node curl, profile list/create;
// see internal/cli/profilegate/classes.go) are issued without --profile: the
// builders for them must not demand one.
func TestGlobalCLICommandsNeedNoProfile(t *testing.T) {
	argv := validateArgs([]string{"claude"}, nil, false, false)
	if namesProfile(argv, "") || len(argv) == 0 || argv[0] != "agent" {
		t.Errorf("agent validate is global; got %v", argv)
	}
	for i, a := range argv {
		if a == "--profile" {
			t.Errorf("agent validate should not carry --profile (arg %d): %v", i, argv)
		}
	}
}
