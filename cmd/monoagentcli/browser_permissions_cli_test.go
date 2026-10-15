package main

import (
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
)

func TestBrowserPermissionsShowAndSet(t *testing.T) {
	bt := newBrowserTest(t, false)
	doc := bt.runJSON("permissions")
	levels := doc["levels"].(map[string]any)
	if levels["coder"] != "dev" || levels["other"] != "dev" || levels["chat"] != "full" || levels["org"] != "full" {
		t.Fatalf("default levels = %v", levels)
	}
	if kinds := doc["kinds"].([]any); len(kinds) != 4 || kinds[0].(map[string]any)["label"] != "Coder chats" || kinds[0].(map[string]any)["isDefault"] != true {
		t.Fatalf("kinds = %v", kinds)
	}
	if cat := doc["catalog"].([]any); len(cat) != 5 || cat[0].(map[string]any)["id"] != "full-no-approvals" || cat[0].(map[string]any)["description"] == "" {
		t.Fatalf("catalog = %v", cat)
	}
	if you := doc["you"].(map[string]any); you["kind"] != "human" || you["level"] != "unrestricted" {
		t.Fatalf("you = %v", you)
	}
	if _, err := bt.run("permissions", "set", "coder", "everything"); exitCodeFor(err) != 3 {
		t.Fatalf("a bad level: %v", err)
	}
	if _, err := bt.run("permissions", "set", "robots", "dev"); exitCodeFor(err) != 3 {
		t.Fatalf("a bad kind: %v", err)
	}
	// The GUI's argv: positionals after "--".
	doc = bt.runJSON("permissions", "set", "--", "coder", "ask")
	if doc["levels"].(map[string]any)["coder"] != "ask" {
		t.Fatalf("after set: %v", doc["levels"])
	}
	doc = bt.runJSON("permissions", "site", "--", "https://bank.example/", "block")
	if sites := doc["sites"].([]any); len(sites) != 1 || sites[0].(map[string]any)["rule"] != "block" || sites[0].(map[string]any)["origin"] != "https://bank.example" {
		t.Fatalf("sites = %v", sites)
	}
	// The allowlist commands are aliases of allow rules.
	bt.runJSON("allow", "add", "https://github.com")
	if got := bt.runJSON("allow", "list")["origins"].([]any); len(got) != 1 || got[0] != "https://github.com" {
		t.Fatalf("allow list = %v", got)
	}
	doc = bt.runJSON("permissions")
	if len(doc["sites"].([]any)) != 2 {
		t.Fatalf("sites after allow add = %v", doc["sites"])
	}
	bt.runJSON("permissions", "site", "https://github.com", "clear")
	if got := bt.runJSON("allow", "list")["origins"].([]any); len(got) != 0 {
		t.Fatalf("allow list after clear = %v", got)
	}
}

func TestBrowserPermissionsAreTheUsers(t *testing.T) {
	bt := newBrowserTest(t, true)
	for _, args := range [][]string{{"permissions", "set", "other", "full"}, {"permissions", "site", "https://x.example", "allow"}} {
		if _, err := bt.run(args...); exitCodeFor(err) != 3 || jsonCode(err) != "operator_only" {
			t.Fatalf("agent %v: %v (%s)", args, err, jsonCode(err))
		}
	}
	doc := bt.runJSON("permissions") // agents may read it
	if you := doc["you"].(map[string]any); you["kind"] != "other" || you["level"] != "dev" {
		t.Fatalf("you = %v", you)
	}
}

// An agent meets each refusal with its own code: browser_off, site_blocked,
// origin_not_allowed, site_needs_approval (with the item to approve).
func TestBrowserPermissionRefusals(t *testing.T) {
	bt := newBrowserTest(t, true)
	db := bt.db().DB
	set := func(level string, sites ...browserctl.SiteRule) {
		p := browserctl.DefaultPermissions()
		p.SetLevel(browserctl.KindOther, level)
		p.Sites = sites
		if err := browserctl.SavePermissions(db, p); err != nil {
			t.Fatal(err)
		}
	}
	set(browserctl.LevelOff)
	for _, args := range [][]string{{"open", "http://localhost:3000/"}, {"tabs"}, {"browsers"}, {"screenshot"}} {
		if _, err := bt.run(args...); jsonCode(err) != "browser_off" || exitCodeFor(err) != 3 {
			t.Fatalf("off, %v: %v (%s)", args, err, jsonCode(err))
		}
	}
	set(browserctl.LevelFull, browserctl.SiteRule{Origin: "https://bank.example", Rule: browserctl.RuleBlock})
	if _, err := bt.run("open", "https://bank.example/"); jsonCode(err) != "site_blocked" {
		t.Fatalf("blocked at full: %v (%s)", err, jsonCode(err))
	}
	if res := bt.runJSON("open", "https://news.example/"); res["url"] != "https://news.example/" {
		t.Fatalf("a real site at full: %v", res)
	}
	set(browserctl.LevelDev)
	if _, err := bt.run("open", "https://news.example/"); jsonCode(err) != "origin_not_allowed" {
		t.Fatalf("dev: %v (%s)", err, jsonCode(err))
	}
	set(browserctl.LevelAsk)
	_, err := bt.run("open", "https://news.example/")
	var fields map[string]any
	if f, ok := err.(jsonErrorFields); ok {
		fields = f.JSONErrorFields()
	}
	id, _ := fields["hil_id"].(string)
	if jsonCode(err) != "site_needs_approval" || id == "" {
		t.Fatalf("ask: %v %v", err, fields)
	}
	if _, err := runHIL(t, bt.cfg, "approve", id); exitCodeFor(err) != 3 {
		t.Fatalf("an agent approved a site: %v", err)
	}
	t.Setenv("CLAUDECODE", "")
	if _, err := runHIL(t, bt.cfg, "approve", id); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDECODE", "1")
	if res := bt.runJSON("open", "https://news.example/"); res["url"] != "https://news.example/" {
		t.Fatalf("open after the user allowed it: %v", res)
	}
}

// A block takes a bare host, rules are stored canonical, and `allow add`
// stores the xn-- form of a Unicode host.
func TestBrowserPermissionsSiteForms(t *testing.T) {
	bt := newBrowserTest(t, false)
	bt.runJSON("permissions", "site", "--", "Bank.Example.", "block")
	bt.runJSON("allow", "add", "HTTPS://Bücher.example:443")
	doc := bt.runJSON("permissions")
	got := map[string]string{}
	for _, s := range doc["sites"].([]any) {
		got[s.(map[string]any)["origin"].(string)] = s.(map[string]any)["rule"].(string)
	}
	if len(got) != 2 || got["bank.example"] != "block" || got["https://xn--bcher-kva.example"] != "allow" {
		t.Fatalf("sites = %v", got)
	}
	if _, err := bt.run("permissions", "site", "--", "github.com", "allow"); exitCodeFor(err) != 3 {
		t.Fatalf("an allow on a bare host: %v", err)
	}
}
