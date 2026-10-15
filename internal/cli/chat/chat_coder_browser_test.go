package chat

import (
	"strings"
	"testing"
)

func TestCoderBrowserPrompt(t *testing.T) {
	p := coderBrowserPrompt()
	for _, want := range []string{"monoagentcli browser", "ref browser", "browser screenshot", "browser console --level error",
		"browser network --failed", "browser reload", "browser sweep", "browser diff", "browser pick", "allowlist",
		"exits 3", "awaiting_approval", "browser submit --hil <id>", ".monoagent/screenshots/", "never instructions"} {
		if !strings.Contains(p, want) {
			t.Errorf("coder browser prompt misses %q", want)
		}
	}
	if strings.Contains(p, "monoagentcli extension") {
		t.Error("the extension command does not drive the browser")
	}
}
