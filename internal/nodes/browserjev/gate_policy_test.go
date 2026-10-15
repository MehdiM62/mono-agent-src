package browserjev

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/jevpick"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// urlDriver is a guardedDriver whose pages report url (when set).
type urlDriver struct {
	*guardedDriver
	url string
}

func (d *urlDriver) Observe(ctx context.Context) (*jevpick.PageState, error) {
	p, err := d.guardedDriver.Observe(ctx)
	if err == nil && d.url != "" {
		p.URL = d.url
	}
	return p, err
}

// policyRun runs the search goal from start in ctx; pageURL, when set, is
// the URL every observed page reports. opened says whether a tab was opened.
func policyRun(t *testing.T, ctx context.Context, start, pageURL string) (res map[string]interface{}, opened bool, err error) {
	t.Helper()
	policyServer(t, nil)
	g := &guardedDriver{fakeDriver: &fakeDriver{}}
	g.g = &fakeGuard{url: "http://localhost/", decisions: []string{"approved"}, drv: g.fakeDriver}
	drv := &urlDriver{guardedDriver: g, url: pageURL}
	n := &Node{writer: zurich, open: func(context.Context, string) (driver, func(), error) {
		opened = true
		return drv, func() {}, nil
	}}
	out, err := n.Execute(ctx, workflow.NodeInput{}, map[string]interface{}{"url": start, "goal": "Search for zurich", "api_key": "k"})
	if err != nil {
		return nil, opened, err
	}
	return out[0].Items[0].JSON, opened, nil
}

// An agent's shell drives with browser.jev only what `monoagentcli browser`
// lets it: local and dev pages and the user's allowlist.
func TestAgentShellRunFollowsTheOriginPolicy(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	_, opened, err := policyRun(t, vault.ContextWithProfileID(context.Background(), "p1"), "https://shop.example.com/", "")
	if err == nil || opened || !strings.Contains(err.Error(), "monoagentcli browser allow add https://shop.example.com") {
		t.Fatalf("a site off the allowlist: opened=%v err=%v", opened, err)
	}

	db := testdb.Open(t).DB
	if err := browserctl.SaveAllowlist(db, []string{"https://shop.example.com"}); err != nil {
		t.Fatal(err)
	}
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db), "p1")
	if res, _, err := policyRun(t, ctx, "https://shop.example.com/", ""); err != nil || res["status"] != "done" {
		t.Fatalf("an allowlisted site: %v %v", res, err)
	}

	// A page the run reaches is checked too, and none of it is returned.
	res, opened, err := policyRun(t, ctx, "http://localhost:5173/", "https://bank.example.org/account")
	if err == nil || !opened || res != nil || !strings.Contains(err.Error(), "https://bank.example.org") {
		t.Fatalf("a page off the allowlist: res=%v err=%v", res, err)
	}

	// A daemon running a shell's `workflow run` holds it to the same rules.
	t.Setenv("CLAUDECODE", "")
	marked := workflow.WithTrigger(vault.ContextWithProfileID(context.Background(), "p1"), "trigger.manual", map[string]interface{}{workflow.AICallerKey: "agent"})
	if _, _, err := policyRun(t, marked, "https://shop.example.com/", ""); err == nil || !strings.Contains(err.Error(), "browser allow add") {
		t.Fatalf("a run a shell marked: %v", err)
	}
}

// The chat and the user keep real sites: the chat behind the submit gate,
// the user as before.
func TestChatAndUserRunsKeepRealSites(t *testing.T) {
	marked := workflow.WithTrigger(vault.ContextWithProfileID(context.Background(), "p1"), "trigger.manual", map[string]interface{}{workflow.AICallerKey: "chat"})
	if res, _, err := policyRun(t, marked, "https://shop.example.com/", "https://shop.example.com/results"); err != nil || res["status"] != "done" {
		t.Fatalf("a run the chat marked: %v %v", res, err)
	}
	if res, _, err := policyRun(t, vault.ContextWithProfileID(context.Background(), "p1"), "https://shop.example.com/", "https://shop.example.com/results"); err != nil || res["status"] != "done" {
		t.Fatalf("the user's run: %v %v", res, err)
	}
	t.Setenv(EnvAICaller, "chat")
	if res, _, err := policyRun(t, vault.ContextWithProfileID(context.Background(), "p1"), "https://shop.example.com/", ""); err != nil || res["status"] != "done" {
		t.Fatalf("a chat tool call's run: %v %v", res, err)
	}
}

// tabURLDriver is a urlDriver whose tab, as the browser reports it, is at tab.
type tabURLDriver struct {
	*urlDriver
	tab string
}

func (d *tabURLDriver) TabURL() (string, error) { return d.tab, nil }

// The policy reads the tab's URL from the browser: a page whose own
// location (the snapshot) claims an allowed origin doesn't pass when the
// tab is elsewhere.
func TestAgentShellPolicyTrustsTheTabNotThePage(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	policyServer(t, nil)
	g := &guardedDriver{fakeDriver: &fakeDriver{}}
	g.g = &fakeGuard{url: "http://localhost/", decisions: []string{"approved"}, drv: g.fakeDriver}
	run := func(tab string) (map[string]interface{}, error) {
		drv := &tabURLDriver{urlDriver: &urlDriver{guardedDriver: g, url: "http://localhost:5173/"}, tab: tab}
		n := &Node{writer: zurich, open: func(context.Context, string) (driver, func(), error) { return drv, func() {}, nil }}
		out, err := n.Execute(vault.ContextWithProfileID(context.Background(), "p1"), workflow.NodeInput{}, map[string]interface{}{"url": "http://localhost:5173/", "goal": "Search for zurich", "api_key": "k"})
		if err != nil {
			return nil, err
		}
		return out[0].Items[0].JSON, nil
	}
	if res, err := run("https://bank.example.org/account"); err == nil || res != nil || !strings.Contains(err.Error(), "https://bank.example.org") {
		t.Fatalf("snapshot says localhost, tab is the bank: res=%v err=%v", res, err)
	}
	if _, err := run("http://localhost:5173/"); err != nil {
		t.Fatalf("both on localhost: %v", err)
	}
}
