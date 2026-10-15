package browserctl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/testdb"
)

// The level × kind matrix: every level, for every kind, on a local page, an
// allowed site, a blocked site and an unknown site.
func TestPolicyByLevelAndKind(t *testing.T) {
	const (
		local   = "http://localhost:3000/app"
		allowed = "https://github.com/x"
		blocked = "https://bank.example/login"
		unknown = "https://news.example/"
	)
	type want struct{ local, allowed, blocked, unknown string } // "" ok, else the error type
	cases := map[string]want{
		LevelFullNoApprovals: {"", "", "blocked", ""},
		LevelFull:            {"", "", "blocked", ""},
		LevelDev:             {"", "", "blocked", "not-allowed"},
		LevelAsk:             {"", "", "blocked", "ask"},
		LevelOff:             {"off", "off", "off", "off"},
	}
	classify := func(err error) string {
		var off *BrowserOffError
		var bl *SiteBlockedError
		var na *NotAllowedError
		var ask *SiteNeedsApprovalError
		switch {
		case err == nil:
			return ""
		case errors.As(err, &off):
			return "off"
		case errors.As(err, &bl):
			return "blocked"
		case errors.As(err, &na):
			return "not-allowed"
		case errors.As(err, &ask):
			return "ask"
		}
		return err.Error()
	}
	for _, kind := range Kinds {
		for level, w := range cases {
			p := DefaultPermissions()
			if err := p.SetLevel(kind, level); err != nil {
				t.Fatal(err)
			}
			p.SetSite("https://github.com", RuleAllow)
			p.SetSite("https://bank.example", RuleBlock)
			pol := p.PolicyFor(kind)
			got := want{classify(pol.Check(local)), classify(pol.Check(allowed)), classify(pol.Check(blocked)), classify(pol.Check(unknown))}
			if got != w {
				t.Errorf("%s at %s: %+v, want %+v", kind, level, got, w)
			}
			gated := pol.SubmitGated(allowed)
			if wantGated := level == LevelFull || level == LevelDev || level == LevelAsk; gated != wantGated {
				t.Errorf("%s at %s: submit on an allowed real site gated=%v", kind, level, gated)
			}
			if pol.SubmitGated(local) {
				t.Errorf("%s at %s: a local submit is gated", kind, level)
			}
		}
	}
	// A block rule wins even over a local page, and file: stays refused at full.
	p := DefaultPermissions()
	p.SetLevel(KindChat, LevelFullNoApprovals)
	p.SetSite("http://localhost:9999", RuleBlock)
	if err := p.PolicyFor(KindChat).Check("http://localhost:9999/"); classify(err) != "blocked" {
		t.Errorf("a blocked local page: %v", err)
	}
	if err := p.PolicyFor(KindChat).Check("file:///etc/passwd"); classify(err) != "not-allowed" {
		t.Errorf("file: at full-no-approvals: %v", err)
	}
	if !p.PolicyFor("").Allowed("https://bank.example/") {
		t.Error("a human is restricted")
	}
}

func TestAgentKindNeverCallsAShellChat(t *testing.T) {
	actAsHuman(t)
	t.Setenv(EnvAgentKind, "")
	t.Setenv(EnvAICaller, "")
	if k := AgentKind(); k != "" {
		t.Fatalf("no markers: %q", k)
	}
	t.Setenv(EnvAICaller, "chat")
	if k := AgentKind(); k != KindChat {
		t.Fatalf("a chat run: %q", k)
	}
	t.Setenv("CLAUDECODE", "1") // a shell that says it is the chat is not
	if k := AgentKind(); k != KindOther {
		t.Fatalf("a shell with %s=chat: %q", EnvAICaller, k)
	}
	t.Setenv(EnvAgentKind, "chat")
	if k := AgentKind(); k != KindOther {
		t.Fatalf("a shell with %s=chat: %q", EnvAgentKind, k)
	}
	asKind(t, KindCoder)
	if k := AgentKind(); k != KindCoder {
		t.Fatalf("a coder turn: %q", k)
	}
	t.Setenv(EnvAgentKind, "")
	t.Setenv("CLAUDECODE", "")
	t.Setenv("MONOMIND_ORG_ROLE", "qa")
	asKind(t, KindOrg)
	t.Setenv(EnvAgentKind, "")
	if k := AgentKind(); k != KindOrg {
		t.Fatalf("an org role: %q", k)
	}
}

func TestPermissionsSeedFromTheOldAllowlist(t *testing.T) {
	db := testdb.Open(t).DB
	if _, err := db.Exec(`INSERT INTO configs (name, config_data) VALUES (?, ?)`, AllowlistSetting,
		`{"origins":["https://github.com","*.corp.example","not an origin"]}`); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPermissions(db)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Sited(RuleAllow); len(got) != 2 || p.Level(KindCoder) != LevelDev || p.Level(KindChat) != LevelFull {
		t.Fatalf("seeded: %+v", p)
	}
	p.SetLevel(KindOrg, LevelAsk)
	if err := SavePermissions(db, p); err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE configs SET config_data = '{"origins":[]}' WHERE name = ?`, AllowlistSetting)
	p, _ = LoadPermissions(db)
	if len(p.Sited(RuleAllow)) != 2 || p.Level(KindOrg) != LevelAsk || p.Level(KindOther) != LevelDev {
		t.Fatalf("after the first write: %+v", p)
	}
	// A stored setting naming only some kinds keeps the others' defaults.
	db.Exec(`UPDATE configs SET config_data = '{"levels":{"coder":"off","bogus":"full","chat":"nope"},"sites":[]}' WHERE name = ?`, PermissionsSetting)
	p, _ = LoadPermissions(db)
	if p.Level(KindCoder) != LevelOff || p.Level(KindChat) != LevelFull || len(p.Sites) != 0 {
		t.Fatalf("partial setting: %+v", p)
	}
}

func TestSiteItemsAreReusedAndApprovedOnce(t *testing.T) {
	actAsHuman(t)
	ctx := context.Background()
	db := testdb.Open(t).DB
	a, err := AskSite(ctx, db, "default", KindCoder, "https://news.example/a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := AskSite(ctx, db, "default", KindCoder, "https://news.example/b", nil); b != a {
		t.Fatal("a pending item for the same origin and kind was not reused")
	}
	if c, _ := AskSite(ctx, db, "default", KindOrg, "https://news.example/", nil); c == a {
		t.Fatal("another kind reused the coder's item")
	}
	it, err := DecideSiteItem(ctx, db, "default", a, true)
	if err != nil || it.Status != "approved" || it.Origin != "https://news.example" {
		t.Fatalf("approve: %+v, %v", it, err)
	}
	if p, _ := LoadPermissions(db); p.Rule("https://news.example") != RuleAllow {
		t.Fatalf("approving added no allow rule: %+v", p.Sites)
	}
	var se *SubmitStateError
	if _, err := DecideSiteItem(ctx, db, "default", a, false); !errors.As(err, &se) {
		t.Fatalf("a second decision: %v", err)
	}
	r, _ := AskSite(ctx, db, "default", KindCoder, "https://other.example/", nil)
	if it, _ := DecideSiteItem(ctx, db, "default", r, false); it.Status != "rejected" {
		t.Fatalf("reject: %+v", it)
	}
	if p, _ := LoadPermissions(db); p.Rule("https://other.example") != "" {
		t.Fatal("rejecting added a rule")
	}
	var nc string
	db.QueryRow(`SELECT node_config FROM hil_pending WHERE id = ?`, r).Scan(&nc)
	if !IsSiteItem(nc) || IsSubmitItem(nc) {
		t.Fatalf("node_config %s", nc)
	}
	if got, _ := GetSiteItem(ctx, db, "default", r); got.Status != "rejected" || got.Origin != "https://other.example" {
		t.Fatalf("get = %+v", got)
	}
	if _, err := AskSite(ctx, db, "", KindCoder, "https://x.example/", nil); err == nil {
		t.Fatal("a site item with no profile")
	}
}

// Every agent shell starts at dev pages, as shipped in v0.141.0 (org roles
// included); the in-app chat keeps any site behind submit approvals. The
// level that lets scripts run anywhere says what that exposes.
func TestDefaultLevelsAndTheRiskyOne(t *testing.T) {
	p := DefaultPermissions()
	for kind, want := range map[string]string{KindCoder: LevelDev, KindOrg: LevelDev, KindOther: LevelDev, KindChat: LevelFull} {
		if got := p.Level(kind); got != want {
			t.Errorf("%s defaults to %s, want %s", kind, got, want)
		}
	}
	if d := Catalog()[0]; d.ID != LevelFullNoApprovals || !strings.Contains(d.Description, "scripts") || !strings.Contains(d.Description, "cookies and storage") {
		t.Errorf("full-no-approvals: %q", d.Description)
	}
}
