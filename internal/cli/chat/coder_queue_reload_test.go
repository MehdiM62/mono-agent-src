package chat

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func withoutReloadSettle(t *testing.T) {
	t.Helper()
	old := queueReloadSettle
	queueReloadSettle = 0
	t.Cleanup(func() { queueReloadSettle = old })
}

// browserSubs is each recorded call's `browser` subcommand.
func browserSubs(calls [][]string) []string {
	var subs []string
	for _, c := range calls {
		if i := slices.Index(c, "browser"); i >= 0 {
			subs = append(subs, c[i+1])
		}
	}
	return subs
}

func readReadme(t *testing.T, dbPath, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(filepath.Dir(dbPath), "coder-queue", id, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// With reload, a bug report turns the capture on and reloads the tab
// before it reads the logs; the screenshot is still the page the person saw.
func TestCoderQueueBugReportReload(t *testing.T) {
	dbPath := queueTestDB(t)
	withoutReloadSettle(t)
	fb := withFakeBrowser(t)

	it := queueAdd(t, dbPath, "--kind", "bug-report", "--reload")
	if got := browserSubs(fb.calls); !slices.Equal(got, []string{"tabs", "screenshot", "console", "reload", "console", "network"}) {
		t.Fatalf("calls = %v", got)
	}
	if !it.Reloaded || !it.LogsEmpty {
		t.Errorf("item reloaded=%v logsEmpty=%v", it.Reloaded, it.LogsEmpty)
	}
	// Nothing was logged across the reload: the prompt says so, and says
	// what to do, without claiming the logs were missed.
	if !strings.Contains(it.Prompt, "reloaded with console and network capture on") || strings.Contains(it.Prompt, "were not captured") {
		t.Errorf("prompt:\n%s", it.Prompt)
	}
	if readme := readReadme(t, dbPath, it.ID); !strings.Contains(readme, "reloaded with console and network capture on before") {
		t.Errorf("README:\n%s", readme)
	}

	// reload only means something for a bug report.
	fb.calls = nil
	queueAdd(t, dbPath, "--kind", "tokens", "--reload")
	if slices.Contains(browserSubs(fb.calls), "reload") {
		t.Errorf("tokens reloaded the tab: %v", browserSubs(fb.calls))
	}
}

func TestCoderQueueBugReportReloadFromStdin(t *testing.T) {
	dbPath := queueTestDB(t)
	withoutReloadSettle(t)
	fb := withFakeBrowser(t)
	cmd := newCoderCmd(testDeps(&testConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true}))
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader(`{"note":"checkout spins forever","reload":true}`))
	cmd.SetArgs([]string{"queue", "add", "--kind", "bug-report", "--stdin-json"})
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add: %v %s", err, out.String())
	}
	if !slices.Contains(browserSubs(fb.calls), "reload") || !strings.Contains(out.String(), `"reloaded":true`) {
		t.Errorf("calls %v, item %s", browserSubs(fb.calls), out.String())
	}
}

// Without reload, empty logs mean nobody was watching the tab: the prompt and
// README say the logs were not captured and how to get them.
func TestCoderQueueBugReportWithoutReload(t *testing.T) {
	dbPath := queueTestDB(t)
	fb := withFakeBrowser(t)

	it := queueAdd(t, dbPath, "--kind", "bug-report")
	if slices.Contains(browserSubs(fb.calls), "reload") {
		t.Fatalf("reloaded without being asked: %v", browserSubs(fb.calls))
	}
	if it.Reloaded || !it.LogsEmpty {
		t.Errorf("item reloaded=%v logsEmpty=%v", it.Reloaded, it.LogsEmpty)
	}
	for _, want := range []string{"were not captured", "`monoagentcli browser reload`", "browser console --level error,warning"} {
		if !strings.Contains(it.Prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, it.Prompt)
		}
	}
	if readme := readReadme(t, dbPath, it.ID); !strings.Contains(readme, "were not captured") {
		t.Errorf("README:\n%s", readme)
	}

	// The console replays what the page logged before capture started; the
	// network log does not, and the prompt says that part is missing.
	fb.console = `{"entries":[{"seq":1,"level":"error","text":"checkout: price table failed to load"}],"next":1}`
	it = queueAdd(t, dbPath, "--kind", "bug-report")
	if it.LogsEmpty || !it.NetworkEmpty || strings.Contains(it.Prompt, "were not captured") ||
		!strings.Contains(it.Prompt, "Network requests made before console and network capture started were not recorded") {
		t.Errorf("console only: logsEmpty=%v networkEmpty=%v prompt:\n%s", it.LogsEmpty, it.NetworkEmpty, it.Prompt)
	}

	// Reloaded, an empty network log is real: no line.
	withoutReloadSettle(t)
	it = queueAdd(t, dbPath, "--kind", "bug-report", "--reload")
	if !it.NetworkEmpty || strings.Contains(it.Prompt, "were not recorded") || strings.Contains(it.Prompt, "logged no errors") {
		t.Errorf("reloaded, console only: prompt:\n%s", it.Prompt)
	}
}
