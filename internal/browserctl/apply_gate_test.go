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

// An agent issued the apply kind, at its default level (ask) and with the
// site allowed, has its submit, fill and upload on a non-local origin held
// for a person, and a new site raises an approval instead of opening.
func TestApplyKindHoldsSubmitFillAndUploadOnARealSite(t *testing.T) {
	setEnv(t, true)
	browserctltest.AsAgentKind(t, browserctl.KindApply)
	if k := browserctl.AgentKind(); k != browserctl.KindApply {
		t.Fatalf("kind = %q", k)
	}
	ctx := context.Background()
	db := testdb.Open(t).DB
	f := browserctltest.New()
	withScreenshot(f)
	f.AddTab("https://jobs.example/apply", "Coder · Agent", true)
	blocked := []any{}
	f.On(extension.CmdSubmitGuard, func(*extension.Command) (any, error) {
		return map[string]any{"armed": true, "origin": "https://jobs.example", "blocked": blocked}, nil
	})

	p := browserctl.DefaultPermissions()
	if got := p.Level(browserctl.KindApply); got != browserctl.LevelAsk {
		t.Fatalf("default apply level = %q", got)
	}
	// A site with no rule is not opened: at ask it raises an approval.
	if _, err := browserctl.Open(browserctl.Options{Profile: "p1", Sender: f, Permissions: &p, DB: db}); err == nil {
		t.Fatal("a new site was opened with no rule at ask")
	}
	p.SetSite("https://jobs.example", browserctl.RuleAllow)
	s, err := browserctl.Open(browserctl.Options{Profile: "p1", Sender: f, Permissions: &p, DB: db})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Gated() {
		t.Fatal("an apply agent's page on a real site is not gated: submit, fill and upload would go through")
	}
	if err := s.BeforeAct(ctx, db); err != nil {
		t.Fatal(err)
	}
	// A submit the page guard blocked is held as an approval item.
	blocked = []any{map[string]any{"seq": 1, "url": "https://jobs.example/apply", "form": map[string]any{
		"action": "https://jobs.example/post", "method": "post", "selector": "form#f", "fields": []any{}}}}
	var aw *browserctl.AwaitingApproval
	if err := s.AfterAct(ctx, db); !errors.As(err, &aw) || aw.Via != "guard" {
		t.Fatalf("submit: %v", err)
	}
	// A fill is held too (browser fill takes this branch whenever Gated).
	aw, err = s.HoldFill(ctx, db, browserctl.FillText, false)
	if err != nil || aw.Via != browserctl.ViaFill {
		t.Fatalf("fill: %+v %v", aw, err)
	}
	// And the same Gated() decides browser upload (cmd/monoagentcli/browser_upload.go).
	if !s.Gated() {
		t.Fatal("upload is not gated")
	}
}
