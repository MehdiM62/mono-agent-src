package browserctl_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/testdb"
)

func expectOrigins(f *browserctltest.Fake, cmdType string) []any {
	var out []any
	for _, c := range f.Calls() {
		if c.Type == cmdType {
			out = append(out, c.Params[browserctl.ExpectOriginParam])
		}
	}
	return out
}

func TestAgentCommandsCarryTheCheckedOrigin(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	tab := f.AddTab("http://localhost:3000/app", "Coder · Agent", true)
	s, err := browserctl.Open(browserctl.Options{Sender: f})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(extension.CmdClick, map[string]interface{}{"selector": "#go"}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Page().EvalCDP("1"); err != nil {
		t.Fatal(err)
	}
	if got := expectOrigins(f, extension.CmdClick); len(got) != 1 || got[0] != "http://localhost:3000" {
		t.Fatalf("click expectOrigin = %v", got)
	}
	if got := expectOrigins(f, "eval_cdp"); len(got) != 1 || got[0] != "http://localhost:3000" {
		t.Fatalf("eval_cdp through Page() expectOrigin = %v", got)
	}
	if got := expectOrigins(f, extension.CmdPageInfo); got[0] != nil {
		t.Fatalf("page_info carries expectOrigin %v", got)
	}

	setEnv(t, false)
	h, _ := browserctl.Open(browserctl.Options{Sender: f, Tab: tab})
	before := len(expectOrigins(f, extension.CmdClick))
	h.Send(extension.CmdClick, map[string]interface{}{"selector": "#go"}, 0)
	if got := expectOrigins(f, extension.CmdClick); got[before] != nil {
		t.Fatalf("a human's click carries expectOrigin %v", got[before])
	}
}

func TestOpenTabThatLandsOnARefusedPageIsClosedUntouched(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	created := 0
	f.On(extension.CmdCreateTab, func(*extension.Command) (any, error) {
		created = f.AddTab("https://login.example/sso", "", true) // the dev page redirected away
		return map[string]any{"tabId": created, "url": "https://login.example/sso"}, nil
	})
	s, _ := browserctl.Open(browserctl.Options{NoTab: true, Sender: f})
	_, err := s.OpenTab("http://localhost:3000/", "")
	var na *browserctl.NotAllowedError
	if !errors.As(err, &na) || na.Origin != "https://login.example" {
		t.Fatalf("open: %v", err)
	}
	if _, open := f.Tab(created); open {
		t.Fatal("the refused tab was left open")
	}
	for _, c := range f.Types() {
		if c == extension.CmdWatchTab || c == extension.CmdReload || c == extension.CmdTabGroup {
			t.Fatalf("the refused tab was touched: %v", f.Types())
		}
	}
}

func TestNavigateThatLandsOnARefusedPage(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	f.On(extension.CmdNavigate, func(c *extension.Command) (any, error) {
		f.SetURL(c.TabID, "https://elsewhere.example/")
		return map[string]any{"tabId": c.TabID, "url": "https://elsewhere.example/"}, nil
	})
	own := f.AddTab("http://localhost:3000/", "Coder · Agent", true)
	s, _ := browserctl.Open(browserctl.Options{Sender: f, Tab: own})
	if _, err := s.Navigate("http://localhost:3000/next"); !errors.As(err, new(*browserctl.NotAllowedError)) {
		t.Fatalf("navigate: %v", err)
	}
	if _, open := f.Tab(own); open {
		t.Fatal("the agent's own tab was left on the refused page")
	}

	users := f.AddTab("http://localhost:5173/", "", false)
	s, _ = browserctl.Open(browserctl.Options{Sender: f, Tab: users})
	if _, err := s.Navigate("http://localhost:5173/x"); !errors.As(err, new(*browserctl.NotAllowedError)) {
		t.Fatalf("navigate: %v", err)
	}
	if _, open := f.Tab(users); !open {
		t.Fatal("a tab the agent did not open was closed")
	}
	calls := f.Calls()
	last := calls[len(calls)-1]
	if last.Type != extension.CmdWatchTab || last.Params["on"] != false {
		t.Fatalf("the refused tab's watch was left on: %+v", last)
	}
}

func TestABlankTabIsOnlyNavigated(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	tab := f.AddTab("about:blank", "Coder · Agent", true)
	s, err := browserctl.Open(browserctl.Options{Sender: f, Tab: tab})
	if err != nil {
		t.Fatal(err)
	}
	if indexOfType(f, extension.CmdWaitLoad) < 0 {
		t.Fatal("the blank tab was not given time to commit its page")
	}
	_, err = s.Send(extension.CmdClick, map[string]interface{}{"selector": "a"}, 0)
	var bt *browserctl.BlankTabError
	if !errors.As(err, &bt) {
		t.Fatalf("click on a blank tab: %v", err)
	}
	if _, err := s.Page().EvalCDP("document.cookie"); !errors.As(err, &bt) {
		t.Fatalf("eval on a blank tab: %v", err)
	}
	if _, err := s.Navigate("http://localhost:3000/"); err != nil {
		t.Fatalf("navigate a blank tab: %v", err)
	}
	if _, err := s.Send(extension.CmdClick, map[string]interface{}{"selector": "a"}, 0); err != nil {
		t.Fatalf("click after navigating: %v", err)
	}
}

func indexOfType(f *browserctltest.Fake, cmdType string) int {
	for i, c := range f.Types() {
		if c == cmdType {
			return i
		}
	}
	return -1
}

// A held submit is bound to its page and to its form as it stood: a value
// changed after the approval, or another page of the origin, refuses it.
func TestAnApprovedSubmitIsBoundToItsFormAndPage(t *testing.T) {
	setEnv(t, true)
	ctx := context.Background()
	db := testdb.Open(t).DB
	f := browserctltest.New()
	withScreenshot(f)
	tab := f.AddTab("https://shop.example/checkout?step=2", "Coder · Agent", true)
	amount := "10"
	f.On("eval_cdp", func(c *extension.Command) (any, error) {
		form := map[string]any{"action": "https://shop.example/pay", "method": "post", "selector": "#pay-form",
			"fields": []any{map[string]any{"name": "amount", "type": "text", "label": "Amount", "value": amount}}}
		if strings.Contains(c.Params["expression"].(string), "const el = document.querySelector") {
			return map[string]any{"result": map[string]any{"found": true, "submit": true, "why": "type=submit", "form": form}}, nil
		}
		return map[string]any{"result": map[string]any{"found": true, "form": form}}, nil
	})
	s, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://shop.example"}})
	probe, err := s.ProbeSubmit("#pay")
	if err != nil || !probe.Submit {
		t.Fatalf("probe: %+v %v", probe, err)
	}
	aw, err := s.Hold(ctx, db, s.URL(), probe.Form, "click", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	it, _ := browserctl.GetSubmitItem(ctx, db, "", aw.HILID)
	if it.FormDigest == "" || it.Form.Fields[0].Value != "10" || it.Form.Submitter != "#pay" {
		t.Fatalf("held item = %+v", it)
	}

	amount = "10000"
	var changed *browserctl.SubmitChangedError
	if err := s.Resubmit(it); !errors.As(err, &changed) || !errors.Is(err, browserctl.ErrNotSent) {
		t.Fatalf("resubmit after the amount changed: %v", err)
	}
	amount = "10"
	f.SetURL(tab, "https://shop.example/checkout/other")
	s2, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://shop.example"}})
	if err := s2.Resubmit(it); !errors.As(err, &changed) {
		t.Fatalf("resubmit on another page of the origin: %v", err)
	}
	f.SetURL(tab, "https://shop.example/checkout?step=3") // the query is not part of the binding
	s3, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://shop.example"}})
	if err := s3.Resubmit(it); err != nil {
		t.Fatalf("resubmit of the unchanged form: %v", err)
	}

	unbound, _ := browserctl.CreateSubmitItem(ctx, db, browserctl.SubmitRequest{URL: "https://shop.example/checkout",
		Form: browserctl.FormSummary{Selector: "#pay-form", Submitter: "#pay"}})
	if err := s3.Resubmit(unbound); !errors.Is(err, browserctl.ErrSubmitUnbound) {
		t.Fatalf("resubmit of an unbound item: %v", err)
	}
}

// A submit held in a frame of the page cannot be repeated (the trigger
// reaches the top frame only), so it is stored unbound and refused before
// the guard is opened anywhere.
func TestASubmitHeldInAFrameIsNotRepeated(t *testing.T) {
	setEnv(t, true)
	ctx := context.Background()
	db := testdb.Open(t).DB
	f := browserctltest.New()
	f.AddTab("https://shop.example/", "Coder · Agent", true)
	s, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://shop.example"}})
	it, err := browserctl.CreateSubmitItem(ctx, db, browserctl.SubmitRequest{URL: "https://shop.example/", Via: "guard",
		FrameID: 2, Page: s.Page(), Form: browserctl.FormSummary{Selector: "#f", Submitter: "#go"}})
	if err != nil || it.FormDigest != "" || it.FrameID != 2 {
		t.Fatalf("item = %+v, %v", it, err)
	}
	if err := s.Resubmit(it); !errors.Is(err, browserctl.ErrNotSent) {
		t.Fatalf("resubmit in a frame: %v", err)
	}
	for _, c := range f.Calls() {
		if c.Type == extension.CmdSubmitGuard && c.Params["action"] == extension.SubmitGuardAllowOnce {
			t.Fatal("the guard was opened for a frame submit")
		}
	}
}

// When the click fails after the guard opened, the guard is closed again,
// so no later submit slips through on that allowance.
func TestAFailedResubmitClosesTheGuardAgain(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	f.AddTab("https://shop.example/", "Coder · Agent", true)
	f.On(extension.CmdClick, func(*extension.Command) (any, error) { return nil, errors.New("Element is detached") })
	s, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://shop.example"}})
	it := browserctl.SubmitItem{URL: "https://shop.example/", Form: browserctl.FormSummary{Submitter: "#go"},
		FormDigest: browserctl.FormDigest(browserctl.FormSummary{})}
	if err := s.Resubmit(it); err == nil {
		t.Fatal("a failed click reported success")
	}
	var actions []any
	for _, c := range f.Calls() {
		if c.Type == extension.CmdSubmitGuard {
			actions = append(actions, c.Params["action"])
		}
	}
	want := []any{"arm", "allow_once", "disarm", "arm"}
	if len(actions) != 4 || actions[0] != want[0] || actions[1] != want[1] || actions[2] != want[2] || actions[3] != want[3] {
		t.Fatalf("guard actions = %v, want %v", actions, want)
	}
}

// An agent's console and network reads keep to the tab's current origin.
func TestAgentReadsKeepToTheTabsOrigin(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	tab := f.AddTab("http://localhost:3000/app", "Coder · Agent", true)
	s, _ := browserctl.Open(browserctl.Options{Sender: f})
	s.Console(-1, nil, 0)
	s.Network(-1, false, 0)
	setEnv(t, false)
	h, _ := browserctl.Open(browserctl.Options{Sender: f, Tab: tab})
	h.Console(-1, nil, 0)
	var got []any
	for _, c := range f.Calls() {
		if c.Type == extension.CmdReadConsole || c.Type == extension.CmdReadNetwork {
			got = append(got, c.Params["origin"])
		}
	}
	if len(got) != 3 || got[0] != "http://localhost:3000" || got[1] != "http://localhost:3000" || got[2] != nil {
		t.Fatalf("origin params = %v", got)
	}
}

// The extension refuses to arm a tab that is still navigating: BeforeAct
// waits for the page, checks where it landed, and arms again.
func TestArmingWaitsForANavigatingTab(t *testing.T) {
	setEnv(t, true)
	db := testdb.Open(t).DB
	f := browserctltest.New()
	tab := f.AddTab("https://shop.example/cart", "Coder · Agent", true)
	arms := 0
	f.On(extension.CmdSubmitGuard, func(c *extension.Command) (any, error) {
		if c.Params["action"] == extension.SubmitGuardArm {
			arms++
			if arms == 1 {
				return nil, errors.New("submit_guard: the tab is navigating; arm it once the page has loaded")
			}
		}
		return map[string]any{"armed": true, "origin": "https://shop.example", "blocked": []any{}}, nil
	})
	s, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: []string{"https://shop.example"}})
	if err := s.BeforeAct(context.Background(), db); err != nil || arms != 2 || indexOfType(f, extension.CmdWaitLoad) < 0 {
		t.Fatalf("before act: %v, %d arms, calls %v", err, arms, f.Types())
	}

	// The navigation it waited for left the allowed pages: refused, not armed.
	arms = 0
	f.SetURL(tab, "https://evil.example/")
	s2 := s
	if err := s2.BeforeAct(context.Background(), db); !errors.As(err, new(*browserctl.NotAllowedError)) || arms != 1 {
		t.Fatalf("before act after leaving: %v, %d arms", err, arms)
	}
}

// Guard records are per origin: a block on the next origin with a seq the
// last one already reported is still held.
func TestBlockedSubmitsAreTrackedPerOrigin(t *testing.T) {
	setEnv(t, true)
	ctx := context.Background()
	db := testdb.Open(t).DB
	f := browserctltest.New()
	withScreenshot(f)
	f.AddTab("https://a.example/", "Coder · Agent", true)
	origin := "https://a.example"
	f.On(extension.CmdSubmitGuard, func(c *extension.Command) (any, error) {
		return map[string]any{"armed": true, "origin": origin, "blocked": []any{map[string]any{"seq": 1, "url": origin + "/",
			"frameId": 0, "form": map[string]any{"action": origin + "/post", "method": "post", "fields": []any{}}}}}, nil
	})
	allow := []string{"https://a.example", "https://b.example"}
	s, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: allow})
	if err := s.BeforeAct(ctx, db); !errors.As(err, new(*browserctl.AwaitingApproval)) {
		t.Fatalf("first block on a: %v", err)
	}
	if err := s.BeforeAct(ctx, db); err != nil {
		t.Fatalf("the same block reported twice: %v", err)
	}
	origin = "https://b.example"
	f.SetURL(s.TabID(), "https://b.example/")
	s2, _ := browserctl.Open(browserctl.Options{Sender: f, Allowlist: allow})
	if err := s2.BeforeAct(ctx, db); !errors.As(err, new(*browserctl.AwaitingApproval)) {
		t.Fatalf("seq 1 on b was taken for a's: %v", err)
	}
}
