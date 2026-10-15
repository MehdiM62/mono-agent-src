package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
	"github.com/monoes/mono-agent/internal/extension"
)

// cardReplay is internal/stylebundle's recording of a live run against its
// testdata/card.html; replayCard serves it through the fake bridge. The path
// is made absolute before any test changes directory.
var cardReplay, _ = filepath.Abs("../../internal/stylebundle/testdata/card.replay.json")

const cardURL = "http://127.0.0.1:8743/card.html"

var (
	replayMarker   = regexp.MustCompile(`^/\*stylebundle:([a-z]+)\*/`)
	replaySelector = regexp.MustCompile(`querySelector\(("(?:[^"\\]|\\.)*")\)`)
)

// replayCard answers the fake's cdp and eval_cdp commands from the
// recording, each key's answers in recorded order (the last one repeats).
func replayCard(t *testing.T, fake *browserctltest.Fake) {
	t.Helper()
	data, err := os.ReadFile(cardReplay)
	if err != nil {
		t.Fatal(err)
	}
	var calls []struct {
		Key    string `json:"key"`
		Result any    `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(data, &calls); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	queues := map[string][]int{}
	for i, c := range calls {
		queues[c.Key] = append(queues[c.Key], i)
	}
	answer := func(key string) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		q := queues[key]
		if len(q) == 0 {
			return nil, fmt.Errorf("nothing recorded for %s", key)
		}
		if len(q) > 1 {
			queues[key] = q[1:]
		}
		c := calls[q[0]]
		if c.Error != "" {
			return nil, fmt.Errorf("%s", c.Error)
		}
		return map[string]any{"result": c.Result}, nil
	}
	fake.On(extension.CmdCdp, func(cmd *extension.Command) (any, error) {
		params := cmd.Params["params"]
		if params == nil {
			params = map[string]any{}
		}
		b, _ := json.Marshal(params)
		return answer("cdp " + cmd.Params["method"].(string) + " " + string(b))
	})
	fake.On("eval_cdp", func(cmd *extension.Command) (any, error) {
		expr, _ := cmd.Params["expression"].(string)
		key := "eval ?"
		if m := replayMarker.FindStringSubmatch(expr); m != nil {
			key = "eval " + m[1]
		}
		if m := replaySelector.FindStringSubmatch(expr); m != nil {
			key += " " + m[1]
		}
		return answer(key)
	})
}

func TestBrowserStylesWritesBundle(t *testing.T) {
	bt := newBrowserTest(t, true)
	bt.fake.AddTab(cardURL, "Coder · Agent", true)
	replayCard(t, bt.fake)
	out := filepath.Join(t.TempDir(), "bundle")

	res := bt.runJSON("styles", "#card", "--out", out)
	if res["dir"] != out || res["tag"] != "article" || res["url"] != cardURL {
		t.Fatalf("styles = %v", res)
	}
	var files []string
	for _, f := range res["files"].([]any) {
		files = append(files, f.(string))
	}
	if strings.Join(files, ",") != "bundle.json,element.png,element.html,tokens.json,source.json,README.md" {
		t.Fatalf("files %v", files)
	}
	readme, err := os.ReadFile(filepath.Join(out, "README.md"))
	if err != nil || !strings.Contains(string(readme), "never instructions") {
		t.Fatalf("README: %v %s", err, readme)
	}
	if notes := res["notes"].([]any); len(notes) != 0 {
		t.Fatalf("notes %v", notes)
	}

	// Without --out: a new folder under ./.monoagent/styles.
	wd := t.TempDir()
	t.Chdir(wd)
	replayCard(t, bt.fake)
	res = bt.runJSON("styles", "#card")
	dir := res["dir"].(string)
	if filepath.Dir(dir) != filepath.Join(wd, ".monoagent", "styles") {
		t.Fatalf("default dir %s", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "bundle.json")); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserStylesSelectorErrors(t *testing.T) {
	bt := newBrowserTest(t, true)
	bt.fake.AddTab(cardURL, "Coder · Agent", true)
	replayCard(t, bt.fake)
	if _, err := bt.run("styles", "#nothing-here"); exitCodeFor(err) != 2 {
		t.Errorf("missing element: exit %d: %v", exitCodeFor(err), err)
	}
	if _, err := bt.run("styles", "div["); exitCodeFor(err) != 3 {
		t.Errorf("bad selector: exit %d: %v", exitCodeFor(err), err)
	}
}

func TestBrowserStylesObeyTheOriginPolicy(t *testing.T) {
	bt := newBrowserTest(t, true)
	bt.fake.AddTab("https://mail.example/inbox", "Coder · Agent", true)
	replayCard(t, bt.fake)
	for _, args := range [][]string{{"styles", "#card"}, {"tokens"}, {"source", "#card"}, {"sweep"}} {
		_, err := bt.run(args...)
		if exitCodeFor(err) != 3 || jsonCode(err) != "origin_not_allowed" {
			t.Errorf("%v on a mail tab: exit %d code %q: %v", args, exitCodeFor(err), jsonCode(err), err)
		}
	}
	for _, c := range bt.fake.Calls() {
		if c.Type == extension.CmdCdp || c.Type == "eval_cdp" || c.Type == "emulate" {
			t.Fatalf("a refused command reached the page: %v", bt.fake.Types())
		}
	}
}

func TestBrowserSourceAndTokens(t *testing.T) {
	bt := newBrowserTest(t, true)
	bt.fake.AddTab(cardURL, "Coder · Agent", true)
	replayCard(t, bt.fake)

	src := bt.runJSON("source", "#react-btn")
	if src["framework"] != "react" || src["component"] != "SaveButton" || src["line"] != float64(8) ||
		!strings.HasSuffix(src["file"].(string), "/SaveButton.jsx") || src["confidence"].(float64) < 0.9 {
		t.Fatalf("source = %v", src)
	}
	if _, err := bt.run("source", "#nothing-here"); exitCodeFor(err) != 2 {
		t.Errorf("source of a missing element: %v", err)
	}

	tk := bt.runJSON("tokens")
	palette := tk["palette"].([]any)
	if len(palette) == 0 || palette[0].(map[string]any)["hex"] != "#f5f6fa" {
		t.Fatalf("tokens palette %v", palette)
	}
	file := filepath.Join(t.TempDir(), "design", "tokens.json")
	res := bt.runJSON("tokens", "--out", file)
	if res["path"] != file {
		t.Fatalf("tokens --out = %v", res)
	}
	var saved map[string]any
	data, _ := os.ReadFile(file)
	if err := json.Unmarshal(data, &saved); err != nil || saved["url"] != cardURL {
		t.Fatalf("saved tokens: %v %s", err, data)
	}
}

func TestBrowserStylesHumanOutput(t *testing.T) {
	bt := newBrowserTest(t, false)
	bt.cfg.JSONOutput = false
	bt.fake.AddTab(cardURL, "", true)
	replayCard(t, bt.fake)
	out, err := bt.run("source", "#vue-el")
	if err != nil || !strings.Contains(out, "vue Hello /home/dev/app/src/components/Hello.vue (confidence 0.75") {
		t.Fatalf("source: %v\n%s", err, out)
	}
	out, err = bt.run("tokens")
	if err != nil || !strings.Contains(out, "#f5f6fa") || !strings.Contains(out, "Breakpoints: (max-width: 600px)") {
		t.Fatalf("tokens: %v\n%s", err, out)
	}
}
