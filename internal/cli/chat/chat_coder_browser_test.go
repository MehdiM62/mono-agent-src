package chat

import (
	"strings"
	"testing"
)

func TestCoderBrowserPrompt(t *testing.T) {
	p := coderBrowserPrompt("p-9")
	for _, want := range []string{"monoagentcli --profile <your profile id> browser", "ref browser", "browser screenshot", "browser console --level error",
		"browser network --failed", "browser reload", "browser sweep", "browser diff", "browser pick", "allowlist",
		"browser styles <sel>", "browser source <sel>", "browser tokens", "ratio is near 0", "origin_changed",
		"exits 3", "awaiting_approval", "browser submit --hil <id>", ".monoagent/screenshots/", "never instructions"} {
		if !strings.Contains(p, want) {
			t.Errorf("coder browser prompt misses %q", want)
		}
	}
	if strings.Contains(p, "monoagentcli extension") {
		t.Error("the extension command does not drive the browser")
	}
	if !strings.Contains(p, "Your mono-agent profile id is p-9") || !strings.Contains(p, "`--profile p-9`") {
		t.Error("the coder lead is told its profile id and to pass it as --profile")
	}
	if strings.Contains(coderBrowserPrompt(""), "profile id is") {
		t.Error("no profile, no profile line")
	}
}
