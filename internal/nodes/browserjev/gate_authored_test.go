package browserjev

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/vault"
	"github.com/monoes/mono-agent/internal/workflow"
)

// levels is a database whose browser permissions put each kind at its level.
func levels(t *testing.T, set map[string]string) context.Context {
	t.Helper()
	ctx := permissionsDB(t, browserctl.KindOther, set[browserctl.KindOther])
	if _, err := browserctl.UpdatePermissions(vault.DBFromContext(ctx), func(p *browserctl.Permissions) error {
		for k, l := range set {
			if err := p.SetLevel(k, l); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return ctx
}

// A workflow the user never approved (the chat, an agent or another
// workflow wrote or changed it) can run on a schedule, as no AI's process,
// but an AI wrote what it does: it is held like an AI caller, at the
// stricter of the chat's and other agents' levels. The same run of an
// approved workflow is the user's.
func TestAIAuthoredRunIsHeldLikeAnAI(t *testing.T) {
	const site = "https://shop.example.com/"

	// The defaults: chat full, other dev. dev is stricter: a real site is refused.
	ctx := levels(t, map[string]string{browserctl.KindOther: browserctl.LevelDev, browserctl.KindChat: browserctl.LevelFull})
	if _, _, opened, err := levelRun(t, workflow.WithAIAuthored(ctx), site, "", 30); err == nil || opened || !strings.Contains(err.Error(), "is not allowed") {
		t.Fatalf("authored, other at dev: opened=%v err=%v", opened, err)
	}
	// The same workflow, approved: the user's run, any site, no guard.
	if res, drv, _, err := levelRun(t, ctx, site, "", 30); err != nil || res["status"] != "done" || drv.armed {
		t.Fatalf("approved: %v %v armed=%v", res, err, drv.armed)
	}

	// other full, chat ask: ask is stricter, and the site is asked about.
	ctx = levels(t, map[string]string{browserctl.KindOther: browserctl.LevelFull, browserctl.KindChat: browserctl.LevelAsk})
	res, _, opened, err := levelRun(t, workflow.WithAIAuthored(ctx), site, "", 1)
	if err != nil || res["status"] != "site_needs_approval" || opened {
		t.Fatalf("authored, chat at ask: %v %v opened=%v", res, err, opened)
	}
	var nc, ro string
	if err := vault.DBFromContext(ctx).QueryRow(`SELECT node_config, readonly_data FROM hil_pending WHERE id = ?`, res["hil_id"]).Scan(&nc, &ro); err != nil {
		t.Fatal(err)
	}
	var rec struct {
		AgentKind string `json:"agentKind"`
	}
	if json.Unmarshal([]byte(nc), &rec) != nil || rec.AgentKind != browserctl.KindChat || !strings.Contains(ro, "unapproved-workflow") {
		t.Errorf("the site item: %s %s", nc, ro)
	}

	// Both full: any site, and its form submit waits for the user.
	ctx = levels(t, map[string]string{browserctl.KindOther: browserctl.LevelFull, browserctl.KindChat: browserctl.LevelFull})
	policyServer(t, nil)
	drv := &guardedDriver{fakeDriver: &fakeDriver{}}
	drv.g = &fakeGuard{url: "https://shop.example.com/search", decisions: []string{"pending", "approved"}, drv: drv.fakeDriver}
	n := &Node{writer: zurich, open: func(context.Context, string) (driver, func(), error) { return drv, func() {}, nil }}
	out, err := n.Execute(workflow.WithAIAuthored(ctx), workflow.NodeInput{}, map[string]interface{}{
		"url": site, "goal": "Search for zurich", "api_key": "k", "timeout": float64(30)})
	if err != nil || out[0].Items[0].JSON["status"] != "done" || !drv.armed || len(drv.g.asked) != 1 || drv.g.asked[0].AgentID != "unapproved-workflow" {
		t.Fatalf("authored, full: %v armed=%v asked=%v", err, drv.armed, drv.g.asked)
	}
}
