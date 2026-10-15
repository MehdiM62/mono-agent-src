package main

import (
	"errors"
	"testing"
)

func namesProfile(argv []string, id string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--profile" && argv[i+1] == id {
			return true
		}
	}
	return false
}

// mustArgs returns a function that fails the test when a builder errs, so
// ok(builder(...)) reads as the argv.
func mustArgs(t *testing.T) func([]string, error) []string {
	return func(argv []string, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return argv
	}
}

// argBuilders are every scoped argv builder, called with the given profile.
func argBuilders(t *testing.T, p string) map[string][]string {
	t.Helper()
	build := func(argv []string, err error) []string {
		if p == "" {
			if !errors.Is(err, errNoProfile) || argv != nil {
				t.Errorf("an empty profile gave %v, %v; want errNoProfile", argv, err)
			}
			return nil
		}
		return mustArgs(t)(argv, err)
	}
	out := map[string][]string{}
	out["workflow list"] = build(scopedCLIArgs(p, "workflow", "list"))
	out["task"] = build(scopedCLIArgs(p, "task", "list"))
	out["org list"] = build(orgCLIRunArgs(p, "", "list"))
	out["org with project"] = build(orgCLIRunArgs(p, "/root", "status", "growth"))
	out["org json args"] = build(orgCLIArgs(p, "/root", []string{"list"}))
	out["status"] = build(statusCLIArgs(p))
	out["automation"] = build(automationCLIArgs(p, "automation", "list"))
	out["workflow import"] = build(workflowImportArgs(p, "f.json", WorkflowImportOptions{}))
	out["workflow delete"] = build(workflowDeleteArgs(p, "wf1"))
	out["oauth connect"] = build(oauthConnectArgs(p, "zendesk_oauth", nil, false))
	out["doctor"] = build(healthArgs(p, healthModeLocal))
	out["doctor fix"] = build(healthFixArgs(p, "fix-id"))
	out["agent install"] = build(agentInstallArgs(p, "hermes", false, ""))
	if p != "" {
		out["chat turn"] = chatTurnArgs(p, "c", "t", "i", "hi", false, false)
	}
	return out
}

// Every scoped command the app issues must name its profile, so the CLI's
// profile gate has nothing to refuse.
func TestScopedCLIArgsNameTheProfile(t *testing.T) {
	const p = "prof-1"
	for name, argv := range argBuilders(t, p) {
		if !namesProfile(argv, p) {
			t.Errorf("%s: %v does not pass --profile %s", name, argv, p)
		}
	}
}

// An empty profile is an error from every builder: there is no default
// profile to fall back to.
func TestScopedCLIArgsRefuseAnEmptyProfile(t *testing.T) {
	argBuilders(t, "")
}

func TestScopedArgsBuildersFollowTheSelection(t *testing.T) {
	a := &App{}
	if _, err := a.scopedArgs("task", "list"); !errors.Is(err, errNoProfile) {
		t.Fatalf("scopedArgs with no selection: %v, want errNoProfile", err)
	}
	a.selectForTest("work")
	got, err := a.scopedArgs("task", "list")
	if err != nil || !namesProfile(got, "work") {
		t.Fatalf("scopedArgs = %v, %v", got, err)
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
	if got := unscopedCLIArgs("profile", "list"); len(got) != 3 || got[0] != "--json" || namesProfile(got, "") {
		t.Errorf("unscopedCLIArgs = %v", got)
	}
}
