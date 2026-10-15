package browserctl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/testdb"
)

// perms is the defaults with kind other (the test's agent: CLAUDECODE) at level.
func perms(t *testing.T, level string) *browserctl.Permissions {
	t.Helper()
	p := browserctl.DefaultPermissions()
	if err := p.SetLevel(browserctl.KindOther, level); err != nil {
		t.Fatal(err)
	}
	return &p
}

// At level ask a new site raises a HIL item instead of a refusal; approving
// it adds an allow rule and the next open passes; rejecting leaves it refused.
func TestAskRaisesASiteItemThatApprovalTurnsIntoARule(t *testing.T) {
	setEnv(t, true)
	ctx := context.Background()
	db := testdb.Open(t).DB
	f := browserctltest.New()
	open := func() error {
		p, err := browserctl.LoadPermissions(db)
		if err != nil {
			t.Fatal(err)
		}
		p.SetLevel(browserctl.KindOther, browserctl.LevelAsk)
		s, err := browserctl.Open(browserctl.Options{NoTab: true, Sender: f, Permissions: &p, DB: db})
		if err != nil {
			return err
		}
		_, err = s.OpenTab("https://news.example/today", "")
		return err
	}
	var pending *browserctl.SiteApprovalPendingError
	if err := open(); !errors.As(err, &pending) || pending.Origin != "https://news.example" || pending.Kind != browserctl.KindOther {
		t.Fatalf("first open: %v", err)
	}
	first := pending.HILID
	if err := open(); !errors.As(err, &pending) || pending.HILID != first {
		t.Fatalf("second open did not reuse the item: %v", err)
	}
	for _, c := range f.Types() {
		if c == extension.CmdCreateTab {
			t.Fatal("a tab was opened before the site was allowed")
		}
	}
	if _, err := browserctl.DecideSiteItem(ctx, db, "", first, true); err != nil {
		t.Fatal(err)
	}
	if err := open(); err != nil {
		t.Fatalf("open after approval: %v", err)
	}

	f2 := browserctltest.New()
	f = f2
	other := func() error {
		p, _ := browserctl.LoadPermissions(db)
		p.SetLevel(browserctl.KindOther, browserctl.LevelAsk)
		s, _ := browserctl.Open(browserctl.Options{NoTab: true, Sender: f, Permissions: &p, DB: db})
		_, err := s.OpenTab("https://ads.example/", "")
		return err
	}
	if err := other(); !errors.As(err, &pending) {
		t.Fatalf("ads: %v", err)
	}
	if _, err := browserctl.DecideSiteItem(ctx, db, "", pending.HILID, false); err != nil {
		t.Fatal(err)
	}
	if err := other(); !errors.As(err, &pending) {
		t.Fatalf("after a rejection the site must still be refused: %v", err)
	}
}

// full-no-approvals never arms the guard: a submit on a real site goes
// through without a HIL item, and a guard armed earlier is let go.
func TestFullNoApprovalsNeverArmsTheGuard(t *testing.T) {
	setEnv(t, true)
	db := testdb.Open(t).DB
	f := browserctltest.New()
	f.AddTab("https://shop.example/cart", "Coder · Agent", true)
	s, err := browserctl.Open(browserctl.Options{Sender: f, Permissions: perms(t, browserctl.LevelFullNoApprovals)})
	if err != nil {
		t.Fatal(err)
	}
	if s.Gated() {
		t.Fatal("a real site is gated at full-no-approvals")
	}
	if err := s.BeforeAct(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Calls() {
		if c.Type == extension.CmdSubmitGuard && c.Params["action"] != extension.SubmitGuardDisarm {
			t.Fatalf("the guard was %v at full-no-approvals", c.Params["action"])
		}
	}
	// full: the same page is reachable, and its submits are gated.
	s, _ = browserctl.Open(browserctl.Options{Sender: f, Permissions: perms(t, browserctl.LevelFull)})
	if !s.Gated() {
		t.Fatal("a real site is not gated at full")
	}
}

// off refuses every command for that kind; a human terminal is unaffected.
func TestOffRefusesTheKindButNotAHuman(t *testing.T) {
	setEnv(t, true)
	f := browserctltest.New()
	tab := f.AddTab("http://localhost:3000/", "Coder · Agent", true)
	var off *browserctl.BrowserOffError
	if _, err := browserctl.Open(browserctl.Options{Sender: f, Permissions: perms(t, browserctl.LevelOff)}); !errors.As(err, &off) {
		t.Fatalf("acting on a local tab at off: %v", err)
	}
	s, err := browserctl.Open(browserctl.Options{NoTab: true, Sender: f, Permissions: perms(t, browserctl.LevelOff)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenTab("http://localhost:3000/", ""); !errors.As(err, &off) {
		t.Fatalf("opening a local page at off: %v", err)
	}
	setEnv(t, false)
	h, err := browserctl.Open(browserctl.Options{Sender: f, Tab: tab, Permissions: perms(t, browserctl.LevelOff)})
	if err != nil || h.Agent() {
		t.Fatalf("a human at off: %v", err)
	}
	if _, err := h.OpenTab("https://bank.example/", ""); err != nil {
		t.Fatalf("a human's open: %v", err)
	}
}
