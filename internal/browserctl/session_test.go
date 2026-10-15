package browserctl_test

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/testdb"
)

func setEnv(t *testing.T, agent bool) {
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
}

var pngBytes = []byte("\x89PNG\r\n\x1a\nfake")

func withScreenshot(f *browserctltest.Fake) {
	f.On(browserctl.CmdScreenshot, func(*extension.Command) (any, error) {
		return map[string]any{"data": base64.StdEncoding.EncodeToString(pngBytes), "mime": "image/png", "width": 800, "height": 600}, nil
	})
}

func TestAgentOpenTabGoesIntoItsGroupAndIsWatched(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	s, err := browserctl.Open(browserctl.Options{NoTab: true, Sender: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenTab("https://example.com/", ""); err == nil {
		t.Fatal("an agent opened https://example.com")
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("a refused open still sent %v", f.Types())
	}
	id, err := s.OpenTab("http://localhost:3000/", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"create_tab", "tab_group", "watch_tab", "reload", "page_info"}
	if got := f.Types(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	tab, _ := f.Tab(id)
	if tab.GroupTitle != "Coder · Agent" {
		t.Fatalf("group = %q", tab.GroupTitle)
	}

	// The next command in this context acts on that tab.
	s2, err := browserctl.Open(browserctl.Options{Sender: f})
	if err != nil {
		t.Fatal(err)
	}
	if s2.TabID() != id || s2.URL() != "http://localhost:3000/" {
		t.Fatalf("default tab = %d %q", s2.TabID(), s2.URL())
	}
}

func TestAgentIsRefusedOnTabsItMayNotDrive(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	bank := f.AddTab("https://bank.example/account", "", true)
	if _, err := browserctl.Open(browserctl.Options{Sender: f}); !errors.Is(err, browserctl.ErrNoTab) {
		t.Fatalf("no group tab: %v", err)
	}
	_, err := browserctl.Open(browserctl.Options{Tab: bank, Sender: f})
	var na *browserctl.NotAllowedError
	if !errors.As(err, &na) || na.Origin != "https://bank.example" {
		t.Fatalf("bank tab: %v", err)
	}
	if strings.Contains(err.Error(), "/account") {
		t.Fatalf("the refusal leaks the path: %v", err)
	}
	// Allowed once on the allowlist.
	if _, err := browserctl.Open(browserctl.Options{Tab: bank, Sender: f, Allowlist: []string{"https://bank.example"}}); err != nil {
		t.Fatal(err)
	}
	// A tab the agent used that navigated away is refused, not swapped.
	local := f.AddTab("http://localhost:5173/", "Coder · Agent", false)
	if _, err := browserctl.Open(browserctl.Options{Tab: local, Sender: f}); err != nil {
		t.Fatal(err)
	}
	f.SetURL(local, "https://accounts.example/login")
	if _, err := browserctl.Open(browserctl.Options{Sender: f}); !errors.As(err, &na) {
		t.Fatalf("navigated-away last tab: %v", err)
	}
}

func TestVisibleTabsForAnAgent(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	f.AddTab("https://mail.example/", "", true)
	own := f.AddTab("https://somewhere.example/", "Coder · Agent", false)
	dev := f.AddTab("http://localhost:8080/", "", false)
	s, err := browserctl.Open(browserctl.Options{NoTab: true, Sender: f})
	if err != nil {
		t.Fatal(err)
	}
	tabs, err := s.VisibleTabs()
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 2 || tabs[0].TabID != own || tabs[1].TabID != dev {
		t.Fatalf("visible = %+v", tabs)
	}
	setEnv(t, false)
	s, _ = browserctl.Open(browserctl.Options{NoTab: true, Sender: f})
	if tabs, _ := s.VisibleTabs(); len(tabs) != 3 {
		t.Fatalf("a human sees %d tabs", len(tabs))
	}
}

func TestHumanDefaultsToTheActiveTab(t *testing.T) {
	setEnv(t, false)
	f := browserctltest.New()
	f.AddTab("chrome://settings", "", false)
	active := f.AddTab("file:///home/me/a.html", "", true)
	s, err := browserctl.Open(browserctl.Options{Sender: f})
	if err != nil {
		t.Fatal(err)
	}
	if s.TabID() != active || s.Agent() {
		t.Fatalf("tab = %d agent=%v", s.TabID(), s.Agent())
	}
}

func TestSubmitGateHoldsBlockedSubmitsOnce(t *testing.T) {
	setEnv(t, true)
	ctx := context.Background()
	db := testdb.Open(t).DB
	f := browserctltest.New()
	withScreenshot(f)
	tab := f.AddTab("https://example.com/new", "Coder · Agent", true)
	blocked := []any{}
	f.On(browserctl.CmdSubmitGuard, func(c *extension.Command) (any, error) {
		return map[string]any{"armed": true, "blocked": blocked}, nil
	})
	s, err := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Gated() {
		t.Fatal("an allowlisted remote page is not gated")
	}
	if err := s.BeforeAct(ctx, db); err != nil {
		t.Fatalf("before: %v", err)
	}
	blocked = []any{map[string]any{"seq": 1, "url": "https://example.com/new", "form": map[string]any{
		"action": "https://example.com/post", "method": "post", "selector": "form#f", "submitter": nil,
		"fields": []any{map[string]any{"name": "body", "type": "textarea", "value": "hello"}}}}}
	err = s.AfterAct(ctx, db)
	var aw *browserctl.AwaitingApproval
	if !errors.As(err, &aw) || aw.Via != "guard" || aw.Origin != "https://example.com" {
		t.Fatalf("after: %v", err)
	}
	it, err := browserctl.GetSubmitItem(ctx, db, "", aw.HILID)
	if err != nil {
		t.Fatal(err)
	}
	if it.Form.Selector != "form#f" || it.TabID != tab || it.Seq != 1 || it.Screenshot == "" {
		t.Fatalf("item = %+v", it)
	}
	if b, err := os.ReadFile(it.Screenshot); err != nil || string(b) != string(pngBytes) {
		t.Fatalf("card screenshot: %v", err)
	}
	// Reported once: the next command does not raise it again.
	s2, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://example.com"}})
	if err := s2.BeforeAct(ctx, db); err != nil {
		t.Fatalf("second before: %v", err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM hil_pending WHERE workflow_id = 'browser.submit'`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d submit items", n)
	}
}

func TestGateFailsClosedWhenTheGuardCannotArm(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	f.AddTab("https://example.com/", "Coder · Agent", true)
	f.On(browserctl.CmdSubmitGuard, func(*extension.Command) (any, error) { return nil, errors.New("Unknown command: submit_guard") })
	s, err := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BeforeAct(context.Background(), testdb.Open(t).DB); err == nil {
		t.Fatal("acted on a gated page with no guard")
	}
}

func TestLocalPagesAreNotGated(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	f.AddTab("http://localhost:3000/", "Coder · Agent", true)
	s, _ := browserctl.Open(browserctl.Options{Sender: f})
	if s.Gated() {
		t.Fatal("localhost is gated")
	}
	if err := s.BeforeAct(context.Background(), testdb.Open(t).DB); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Types() {
		if c == browserctl.CmdSubmitGuard {
			t.Fatalf("the guard was armed on localhost: %v", f.Types())
		}
	}
}

func TestResubmitClicksTheRecordedSubmitter(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	f.AddTab("https://example.com/", "Coder · Agent", true)
	s, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://example.com"}})
	if err := s.Resubmit(browserctl.SubmitItem{Form: browserctl.FormSummary{Submitter: "#send"}}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	last2 := calls[len(calls)-2:]
	if last2[0].Type != "submit_guard" || last2[0].Params["action"] != "allow_once" || last2[1].Type != "click" || last2[1].Params["selector"] != "#send" {
		t.Fatalf("calls = %+v", last2)
	}
}

func TestWriteScreenshot(t *testing.T) {
	dir := t.TempDir()
	shot := &browserctl.Shot{Data: pngBytes, Mime: "image/png"}
	a, err := browserctl.WriteScreenshot(dir, "", shot)
	if err != nil {
		t.Fatal(err)
	}
	b, err := browserctl.WriteScreenshot(dir, "", shot)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || filepath.Dir(a) != filepath.Join(dir, ".monoagent", "screenshots") || filepath.Dir(b) != filepath.Dir(a) || !strings.HasSuffix(a, "-1.png") {
		t.Fatalf("paths %s %s", a, b)
	}
	c, err := browserctl.WriteScreenshot(dir, "out/x.png", shot)
	if err != nil || c != filepath.Join(dir, "out", "x.png") {
		t.Fatalf("--out path %s, %v", c, err)
	}
}
