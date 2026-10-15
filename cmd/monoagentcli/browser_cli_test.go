package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/testdb"
)

// browserTest runs browser commands against a fake bridge, as an agent
// (CLAUDECODE set) or as the user's own terminal.
type browserTest struct {
	t    *testing.T
	cfg  *globalConfig
	fake *browserctltest.Fake
}

func newBrowserTest(t *testing.T, agent bool) *browserTest {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, k := range browserctl.AgentContextEnv() {
		t.Setenv(k, "")
	}
	for _, k := range []string{"MONOAGENT_CONVERSATION_ID", "MONOAGENT_AGENT_ROLE", "MONOMIND_ORG_NAME", browserctl.EnvCoderTitle} {
		t.Setenv(k, "")
	}
	if agent {
		t.Setenv("CLAUDECODE", "1")
	}
	fake := browserctltest.New()
	fake.On(browserctl.CmdScreenshot, func(*extension.Command) (any, error) {
		return map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("\x89PNG fake")), "mime": "image/png", "width": 375, "height": 812}, nil
	})
	old := browserBridge
	browserBridge = func() (browserctl.Sender, error) { return fake, nil }
	t.Cleanup(func() { browserBridge = old })
	return &browserTest{t: t, cfg: &globalConfig{DBPath: testdb.Path(t), JSONOutput: true}, fake: fake}
}

// run executes `browser args…` and returns stdout and the error.
func (bt *browserTest) run(args ...string) (string, error) {
	var out bytes.Buffer
	cmd := newBrowserCmd(bt.cfg)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true // as under the root command
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func (bt *browserTest) runJSON(args ...string) map[string]any {
	bt.t.Helper()
	out, err := bt.run(args...)
	if err != nil {
		bt.t.Fatalf("browser %v: %v", args, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		bt.t.Fatalf("browser %v printed %q: %v", args, out, err)
	}
	return m
}

func (bt *browserTest) db() *storage.Database {
	db, err := storage.NewDatabase(bt.cfg.DBPath)
	if err != nil {
		bt.t.Fatal(err)
	}
	bt.t.Cleanup(func() { db.Close() })
	return db
}

func jsonCode(err error) string {
	var f jsonErrorFields
	if errors.As(err, &f) {
		code, _ := f.JSONErrorFields()["code"].(string)
		return code
	}
	return jsonErrorCode(err)
}

func TestBrowserAgentOriginPolicy(t *testing.T) {
	bt := newBrowserTest(t, true)
	_, err := bt.run("open", "https://example.com/")
	if exitCodeFor(err) != 3 || jsonCode(err) != "origin_not_allowed" {
		t.Fatalf("open example.com: exit %d code %q: %v", exitCodeFor(err), jsonCode(err), err)
	}
	if len(bt.fake.Calls()) != 0 {
		t.Fatalf("a refused open reached the browser: %v", bt.fake.Types())
	}
	res := bt.runJSON("open", "http://localhost:3000/")
	id := int(res["tabId"].(float64))
	if tab, _ := bt.fake.Tab(id); tab.GroupTitle != "Coder · Agent" || res["url"] != "http://localhost:3000/" {
		t.Fatalf("open = %v, tab group %q", res, tab.GroupTitle)
	}
	// The next command without --tab uses that tab; one on another page is refused.
	if got := bt.runJSON("click", "#go"); got["clicked"] != "#go" || int(got["tabId"].(float64)) != id {
		t.Fatalf("click = %v", got)
	}
	other := bt.fake.AddTab("https://mail.example/inbox", "", true)
	if _, err := bt.run("eval", "1", "--tab", itoa(other)); exitCodeFor(err) != 3 {
		t.Fatalf("eval on a mail tab: %v", err)
	}
	if tabs := bt.runJSON("tabs")["tabs"].([]any); len(tabs) != 1 {
		t.Fatalf("an agent sees %d tabs", len(tabs))
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestBrowserHeldSubmitExits3AndIsApprovedOnce(t *testing.T) {
	bt := newBrowserTest(t, true)
	if err := browserctl.SaveAllowlist(bt.db().DB, []string{"https://example.com"}); err != nil {
		t.Fatal(err)
	}
	tab := bt.fake.AddTab("https://example.com/compose", "Coder · Agent", true)
	bt.fake.On("eval_cdp", func(*extension.Command) (any, error) {
		return map[string]any{"result": map[string]any{"found": true, "submit": true, "why": "type=submit",
			"form": map[string]any{"action": "https://example.com/post", "method": "post", "selector": "#f",
				"fields": []any{map[string]any{"name": "body", "type": "textarea", "value": "hi"}}}}}, nil
	})

	out, err := bt.run("click", "#send")
	if exitCodeFor(err) != 3 {
		t.Fatalf("held click: exit %d: %v", exitCodeFor(err), err)
	}
	var held map[string]any
	if jerr := json.Unmarshal([]byte(out), &held); jerr != nil || held["status"] != "awaiting_approval" {
		t.Fatalf("held click printed %q", out)
	}
	hilID, _ := held["hil_id"].(string)
	for _, c := range bt.fake.Calls() {
		if c.Type == extension.CmdClick {
			t.Fatal("the submit click reached the page")
		}
	}

	// It is in `hil list` for the HIL page and the badge.
	items, err := runHIL(t, bt.cfg, "list")
	if err != nil || len(items) != 1 || items[0].ID != hilID || items[0].WorkflowID != "browser.submit" || items[0].Kind != "" {
		t.Fatalf("hil list = %+v, %v", items, err)
	}
	if items[0].ReadonlyData["origin"] != "https://example.com" || items[0].ReadonlyData["screenshot"] == "" {
		t.Fatalf("card = %v", items[0].ReadonlyData)
	}

	if _, err := bt.run("submit", "--hil", hilID); exitCodeFor(err) != 3 || jsonCode(err) != "submit_pending" {
		t.Fatalf("submit before approval: %v (%s)", err, jsonCode(err))
	}
	if _, err := runHIL(t, bt.cfg, "approve", hilID[:8]); err != nil {
		t.Fatalf("hil approve: %v", err)
	}
	res := bt.runJSON("submit", "--hil", hilID)
	if res["status"] != "submitted" || int(res["tabId"].(float64)) != tab {
		t.Fatalf("submit = %v", res)
	}
	calls := bt.fake.Calls()
	var allowOnce, click bool
	for _, c := range calls {
		allowOnce = allowOnce || (c.Type == browserctl.CmdSubmitGuard && c.Params["action"] == "allow_once")
		click = click || (c.Type == extension.CmdClick && c.Params["selector"] == "#send" && allowOnce)
	}
	if !allowOnce || !click {
		t.Fatalf("submit did not let the guard through and click: %v", bt.fake.Types())
	}
	if _, err := bt.run("submit", "--hil", hilID); jsonCode(err) != "submit_consumed" {
		t.Fatalf("second submit: %v (%s)", err, jsonCode(err))
	}
}

func TestBrowserSubmitHandsTheApprovalBackWhenTheTabMoved(t *testing.T) {
	bt := newBrowserTest(t, true)
	db := bt.db().DB
	tab := bt.fake.AddTab("https://example.com/compose", "Coder · Agent", true)
	it, err := browserctl.CreateSubmitItem(t.Context(), db, browserctl.SubmitRequest{
		URL: "https://example.com/compose", Via: "click", TabID: tab, Instance: bt.fake.Instance,
		Form: browserctl.FormSummary{Submitter: "#send"},
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE hil_pending SET status = 'approved' WHERE id = ?`, it.ID)
	bt.fake.SetURL(tab, "http://localhost:3000/")
	if _, err := bt.run("submit", "--hil", it.ID); exitCodeFor(err) != 3 {
		t.Fatalf("submit on a moved tab: %v", err)
	}
	if got, _ := browserctl.GetSubmitItem(t.Context(), db, "default", it.ID); got.Status != "approved" {
		t.Fatalf("approval not handed back: %s", got.Status)
	}
}

func TestBrowserAllowIsTheUsers(t *testing.T) {
	bt := newBrowserTest(t, true)
	_, err := bt.run("allow", "add", "https://example.com")
	if exitCodeFor(err) != 3 || jsonCode(err) != "operator_only" {
		t.Fatalf("agent allow add: %v (%s)", err, jsonCode(err))
	}
	if got := bt.runJSON("allow", "list")["origins"].([]any); len(got) != 0 {
		t.Fatalf("list = %v", got)
	}
	t.Setenv("CLAUDECODE", "")
	if _, err := bt.run("allow", "add", "example.com"); exitCodeFor(err) != 3 {
		t.Fatalf("bare host accepted: %v", err)
	}
	if got := bt.runJSON("allow", "add", "https://Example.com/"); got["added"] != "https://example.com" {
		t.Fatalf("add = %v", got)
	}
	bt.runJSON("allow", "add", "*.corp.example")
	if got := bt.runJSON("allow", "list")["origins"].([]any); len(got) != 2 {
		t.Fatalf("list = %v", got)
	}
	bt.runJSON("allow", "remove", "https://example.com")
	if _, err := bt.run("allow", "remove", "https://example.com"); exitCodeFor(err) != 2 {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestBrowserScreenshotEmulatesForTheShotOnly(t *testing.T) {
	bt := newBrowserTest(t, false)
	dir := t.TempDir()
	t.Chdir(dir)
	bt.fake.AddTab("https://anything.example/", "", true)
	res := bt.runJSON("screenshot", "--width", "375", "--media", "dark")
	path, _ := res["path"].(string)
	if filepath.Dir(path) != filepath.Join(dir, ".monoagent", "screenshots") || !strings.HasSuffix(path, ".png") || res["width"] != 375.0 {
		t.Fatalf("screenshot = %v", res)
	}
	var emulate []extension.Command
	for _, c := range bt.fake.Calls() {
		if c.Type == browserctl.CmdEmulate {
			emulate = append(emulate, c)
		}
	}
	if len(emulate) != 2 || emulate[0].Params["width"] != 375 || emulate[1].Params["reset"] != true {
		t.Fatalf("emulate calls = %+v", emulate)
	}
	media, _ := emulate[0].Params["media"].(map[string]interface{})
	if media["prefersColorScheme"] != "dark" {
		t.Fatalf("media = %v", emulate[0].Params)
	}
	if _, err := bt.run("screenshot", "--full", "--element", "x"); exitCodeFor(err) != 3 {
		t.Fatalf("--full with --element: %v", err)
	}
}

func TestBrowserConsoleWatchesThenReads(t *testing.T) {
	bt := newBrowserTest(t, false)
	bt.fake.AddTab("http://localhost:5173/", "", true)
	bt.fake.On(browserctl.CmdReadConsole, func(c *extension.Command) (any, error) {
		return map[string]any{"entries": []any{map[string]any{"seq": 3, "level": "error", "text": "boom", "url": "http://localhost:5173/app.js", "line": 9}}, "next": 3}, nil
	})
	res := bt.runJSON("console", "--level", "error", "--since", "2")
	entries := res["entries"].([]any)
	if len(entries) != 1 || res["next"] != 3.0 {
		t.Fatalf("console = %v", res)
	}
	types := bt.fake.Types()
	if i := indexOf(types, browserctl.CmdWatchTab); i < 0 || i > indexOf(types, browserctl.CmdReadConsole) {
		t.Fatalf("calls = %v", types)
	}
	read := bt.fake.Calls()[indexOf(types, browserctl.CmdReadConsole)]
	if read.Params["since"] != 2 || len(read.Params["levels"].([]string)) != 1 {
		t.Fatalf("read_console params = %v", read.Params)
	}
}

func indexOf(xs []string, x string) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}

func TestBrowserWithoutBridgeOrTab(t *testing.T) {
	bt := newBrowserTest(t, false)
	if _, err := bt.run("click", "a"); exitCodeFor(err) != 2 {
		t.Fatalf("no tab: exit %d: %v", exitCodeFor(err), err)
	}
	browserBridge = func() (browserctl.Sender, error) { return nil, browserctl.ErrNoBridge }
	if _, err := bt.run("tabs"); exitCodeFor(err) != 4 {
		t.Fatalf("no bridge: exit %d: %v", exitCodeFor(err), err)
	}
}
