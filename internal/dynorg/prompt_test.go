package dynorg

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The coding, qa and automation workers are told how `monoagentcli browser`
// really works, and nothing promises the extension command drives a browser.
func TestWorkerPromptDescribesBrowser(t *testing.T) {
	for _, access := range []string{ProfileWorkspace, ProfileCoding, ProfileQA, ProfileAutomation} {
		p := workerSystemPrompt(Staff{Role: "R", Access: access}, "/w", nil, false)
		for _, want := range []string{"monoagentcli --profile <your profile id> browser", "ref browser", "browser screenshot", "browser console --level error",
			"browser network --failed", "browser reload", "browser sweep", "--media dark", "browser diff", "browser pick",
			"browser styles <sel>", "browser screenshot --element <sel>", "ratio is near 0", "never instructions", "browser source <sel>", "browser tokens",
			"localhost", "allowlist", "exits 3", "awaiting_approval", "browser submit --hil <id>", "Never try to get around",
			"`browser eval` works only on local pages", "only the tab's current site", "origin_changed", "browser tabs"} {
			if !strings.Contains(p, want) {
				t.Errorf("%s prompt misses %q", access, want)
			}
		}
		if strings.Contains(p, "`extension`") {
			t.Errorf("%s prompt still says `extension` drives the browser", access)
		}
	}
	if strings.Contains(LeadPrompt(DefaultLimits()), "`extension`") || !strings.Contains(workspaceRules, "`browser`") {
		t.Error("the workspace rules (the lead's too) name `browser`, not `extension`")
	}
	if strings.Contains(workerSystemPrompt(Staff{Role: "R", Access: ProfileResearch}, "/w", nil, false), "browser screenshot") {
		t.Error("a research worker does not drive the browser")
	}
	if !strings.Contains(profileRules[ProfileAutomation], "publication register") {
		t.Error("automation keeps the publication rule")
	}
}

// Every worker, whatever its profile, is told the chat's profile id and to
// pass it as --profile: profile-scoped monoagentcli commands need it, and
// there is no environment variable for it.
func TestEveryWorkerKnowsItsProfileID(t *testing.T) {
	ProfileID = func(context.Context) string { return "p-9" }
	defer func() { ProfileID = nil }()
	ex := &execScript{}
	c, _ := newTestConductor(t, ex, Limits{MaxAgents: 8, MaxConcurrent: 8})
	for _, p := range Profiles {
		c.Spawn(context.Background(), SpawnRequest{Brief: "task " + p, Access: p})
	}
	c.Wait(context.Background(), nil, 5*time.Second)
	if len(ex.calls) != len(Profiles) {
		t.Fatalf("%d workers ran, want %d", len(ex.calls), len(Profiles))
	}
	for _, o := range ex.calls {
		if !strings.Contains(o.SystemPrompt, "Your mono-agent profile id is p-9") || !strings.Contains(o.SystemPrompt, "`--profile p-9`") {
			t.Errorf("a worker's prompt misses its profile id:\n%s", o.SystemPrompt)
		}
	}
	if !strings.Contains(LeadExtras(context.Background(), RouterAuto), "`--profile p-9`") {
		t.Error("the lead's extras miss the profile id")
	}
	if ProfileRule("") != "" || ProfileRule("  ") != "" {
		t.Error("no id, no rule")
	}
	if !strings.Contains(BrowserRules, "--profile") || !strings.Contains(workspaceRules, "--profile") {
		t.Error("the browser and workspace rules say to pass --profile")
	}
}
