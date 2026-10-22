package browserctl

import (
	"testing"

	"github.com/monoes/mono-agent/internal/testdb"
)

func TestApplyKindDefaultsToAsk(t *testing.T) {
	p := DefaultPermissions()
	if got := p.Level(KindApply); got != LevelAsk {
		t.Fatalf("apply default level = %q, want ask", got)
	}
	if got := DefaultLevels[KindCoder]; got != LevelDev {
		t.Fatalf("coder default changed to %q", got)
	}
}

// apply can never reach full-no-approvals, by the setter, the encoder or a
// hand-edited stored row; each assertion fails if levelAllowed is removed.
func TestApplyNeverFullNoApprovals(t *testing.T) {
	p := DefaultPermissions()
	if err := p.SetLevel(KindApply, LevelFullNoApprovals); err == nil {
		t.Fatal("SetLevel(apply, full-no-approvals) was accepted")
	}
	if err := p.SetLevel(KindApply, LevelFull); err != nil {
		t.Fatalf("apply full: %v", err)
	}
	p.Levels[KindApply] = LevelFullNoApprovals // a caller poking the map
	if _, _, err := encodePermissions(p); err == nil {
		t.Error("encodePermissions stored apply=full-no-approvals")
	}
	if got := p.Level(KindApply); got != LevelAsk {
		t.Errorf("Level(apply) with the map poked = %q, want ask", got)
	}
	db := testdb.Open(t).DB
	if _, err := db.Exec(`INSERT INTO configs (name, config_data, created_at, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, PermissionsSetting,
		`{"schema":2,"levels":{"apply":"full-no-approvals","coder":"full-no-approvals"},"sites":[]}`); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPermissions(db)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Level(KindApply); got != LevelAsk {
		t.Errorf("stored apply=full-no-approvals loads as %q, want the default ask", got)
	}
	if got := loaded.Level(KindCoder); got != LevelFullNoApprovals {
		t.Errorf("coder level lost: %q", got)
	}
}

func TestApplySubmitIsHeldAtEveryAllowedLevel(t *testing.T) {
	const site = "https://jobs.example/apply"
	for _, level := range []string{LevelFull, LevelDev, LevelAsk} {
		p := DefaultPermissions()
		p.SetSite("https://jobs.example", RuleAllow)
		if err := p.SetLevel(KindApply, level); err != nil {
			t.Fatal(err)
		}
		pol := p.PolicyFor(KindApply)
		if !pol.SubmitGated(site) {
			t.Errorf("apply at %s: a submit on a real site is not held", level)
		}
	}
}

func TestApplyRespectsSiteRules(t *testing.T) {
	p := DefaultPermissions()
	pol := p.PolicyFor(KindApply)
	if err := pol.Check("https://jobs.example/x"); err == nil {
		t.Error("apply at ask drove a new site without a person's yes")
	}
	p.SetSite("https://jobs.example", RuleAllow)
	if err := p.PolicyFor(KindApply).Check("https://jobs.example/x"); err != nil {
		t.Errorf("allowed site refused: %v", err)
	}
	p.SetSite("jobs.example", RuleBlock)
	if err := p.PolicyFor(KindApply).Check("https://jobs.example/x"); err == nil {
		t.Error("blocked site allowed")
	}
}

func TestApplyKindTokenIsVouched(t *testing.T) {
	actAsHuman(t)
	t.Setenv(EnvAICaller, "")
	t.Setenv("CLAUDECODE", "1")
	asKind(t, KindApply)
	if k := AgentKind(); k != KindApply {
		t.Fatalf("an apply token: kind %q", k)
	}
	// A coder token does not vouch for apply (nor the reverse).
	t.Setenv(EnvAgentKind, KindCoder)
	if k := AgentKind(); k != KindOther {
		t.Fatalf("apply token claimed as coder: %q", k)
	}
	// Unproven apply: other, with the stricter of the two levels.
	t.Setenv(EnvAgentKind, KindApply)
	t.Setenv(EnvAgentKindToken, "")
	c := AgentCaller()
	if c.Kind != KindOther || c.Declared != KindApply {
		t.Fatalf("declared apply with no token: %+v", c)
	}
	p := DefaultPermissions() // other=dev is stricter than apply=ask
	if got := p.LevelOf(c); got != LevelDev {
		t.Errorf("level of an unproven apply = %q, want dev", got)
	}
}
