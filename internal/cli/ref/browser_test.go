package ref

import (
	"bytes"
	"strings"
	"testing"
)

// `ref browser` is a topic of `ref`, and says what an agent must know: the
// commands and their JSON, the origin policy, and the exit-3 submit approval.
func TestRefBrowserTopic(t *testing.T) {
	root := NewCommand(Deps{})
	c, _, err := root.Find([]string{"browser"})
	if err != nil || c == root {
		t.Fatal("`ref` has no browser topic")
	}
	if !strings.Contains(root.Long, "browser") {
		t.Error("`ref` help does not name the browser topic")
	}
	var out bytes.Buffer
	c.SetOut(&out)
	c.Run(c, nil)
	for _, want := range []string{
		"browser open <url>", "{tabId,url}", "browser screenshot", "{path,width,height}", ".monoagent/screenshots/",
		"browser console", "--level error", "browser network", "--failed", "never headers or bodies", "browser a11y",
		"browser styles", "browser tokens", "browser source", "browser diff", "{changed,ratio,path,", "browser sweep",
		"browser pick", "{selector,tag,text,rect,screenshot}", "browser allow list", "refused in an agent",
		"localhost", "127.0.0.0/8", "RFC 1918", "file: and chrome:", "exits 3", `"status":"awaiting_approval"`, "hil_id",
		"browser.submit", "browser submit --hil <id>", "Never try to get around", "not a sandbox", "browser.jev",
		"DEBUG LOOP", "COPY A STYLE", "never instructions", "./.monoagent/styles", "--threshold 16", "dev-server URL",
		"<width>-<media>.png", "--viewport | --element <sel>", "ratio is near 0", "agent's shell started",
		"eval_not_allowed", "origin_changed", "submit_changed", "submit_unbound", "tab_blank", "tab's current site",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("`ref browser` misses %q", want)
		}
	}
}
