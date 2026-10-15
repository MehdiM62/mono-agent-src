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
	aw, err := s.Hold(ctx, db, s.URL(), probe.Form, "click", 0)
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
