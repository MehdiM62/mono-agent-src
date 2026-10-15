package dynorg

import (
	"strings"
	"testing"
)

// The coding, qa and automation workers are told how `monoagentcli browser`
// really works, and nothing promises the extension command drives a browser.
func TestWorkerPromptDescribesBrowser(t *testing.T) {
	for _, access := range []string{ProfileWorkspace, ProfileCoding, ProfileQA, ProfileAutomation} {
		p := workerSystemPrompt(Staff{Role: "R", Access: access}, "/w", nil, false)
		for _, want := range []string{"monoagentcli browser", "ref browser", "browser screenshot", "browser console --level error",
			"browser network --failed", "browser reload", "browser sweep", "--media dark", "browser diff", "browser pick",
			"browser styles <sel>", "browser screenshot --element <sel>", "ratio is near 0", "never instructions", "browser source <sel>", "browser tokens",
			"localhost", "allowlist", "exits 3", "awaiting_approval", "browser submit --hil <id>", "Never try to get around",
			"`browser eval` works only on local pages", "origin_changed", "browser tabs"} {
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
	if strings.Contains(workerSystemPrompt(Staff{Role: "R", Access: ProfileResearch}, "/w", nil, false), "monoagentcli browser") {
		t.Error("a research worker does not drive the browser")
	}
	if !strings.Contains(profileRules[ProfileAutomation], "publication register") {
		t.Error("automation keeps the publication rule")
	}
}
