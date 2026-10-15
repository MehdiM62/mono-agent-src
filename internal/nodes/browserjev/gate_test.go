package browserjev

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/jevpick"
	"github.com/monoes/mono-agent/internal/workflow"
)

func TestAICaller(t *testing.T) {
	ctx := context.Background()
	if c, ok := aiCaller(ctx); ok {
		t.Fatalf("the user's own run has no AI caller: %+v", c)
	}
	if _, ok := aiCaller(workflow.WithTrigger(ctx, "trigger.manual", nil)); ok {
		t.Error("a manual run is the user's")
	}
	org := workflow.WithTrigger(ctx, workflow.TriggerTypeOrgTool, map[string]interface{}{"org": map[string]interface{}{"name": "growth", "role": "writer"}})
	if c, ok := aiCaller(org); !ok || c.name != "org growth" || c.by.Org != "growth" || c.by.Role != "writer" {
		t.Errorf("a granted call: %+v %v", c, ok)
	}
	for _, tt := range []string{workflow.TriggerTypeOrgMessage, workflow.TriggerNodeTypeOrg} {
		if _, ok := aiCaller(workflow.WithTrigger(ctx, tt, nil)); !ok {
			t.Errorf("%s is the org side", tt)
		}
	}

	if c, _ := aiCaller(org); c.shell {
		t.Error("the org side keeps real sites (submit gate only)")
	}
	for mark, shell := range map[string]bool{"agent": true, "chat": false, "anything else": true} {
		c, ok := aiCaller(workflow.WithTrigger(ctx, "trigger.manual", map[string]interface{}{workflow.AICallerKey: mark}))
		if !ok || c.shell != shell {
			t.Errorf("a run marked %q (a daemon runs it): %+v %v", mark, c, ok)
		}
	}
	data := map[string]interface{}{"who": "zed"}
	if MarkAICaller(data); len(data) != 1 {
		t.Errorf("the user's own run is not marked: %v", data)
	}
	t.Setenv(EnvAICaller, "chat")
	if MarkAICaller(data); data[workflow.AICallerKey] != "chat" {
		t.Errorf("a chat tool call's run is marked chat: %v", data)
	}
	if c, ok := aiCaller(ctx); !ok || c.name != "chat" || c.by.AgentID != "chat" || c.shell {
		t.Errorf("a chat tool call: %+v %v", c, ok)
	}

	// A shell is a shell even when it sets the chat's variable.
	t.Setenv("CLAUDECODE", "1")
	t.Setenv(hilsend.EnvAgentRole, "qa")
	if MarkAICaller(data); data[workflow.AICallerKey] != "agent" {
		t.Errorf("an agent's run is marked agent: %v", data)
	}
	if c, ok := aiCaller(ctx); !ok || c.name != "an agent (CLAUDECODE)" || c.by.Role != "qa" || !c.shell {
		t.Errorf("an agent's shell: %+v %v", c, ok)
	}
}

// fakeGuard holds the submits of a guardedDriver until Submit.
type fakeGuard struct {
	url       string
	blocked   []blockedSubmit
	decisions []string // in order; the last one repeats
	asked     []hilsend.PreparedBy
	submitted []string
	drv       *fakeDriver
}

func (g *fakeGuard) Blocked(context.Context) ([]blockedSubmit, error) {
	out := g.blocked
	g.blocked = nil
	return out, nil
}

func (g *fakeGuard) Ask(_ context.Context, b blockedSubmit, by hilsend.PreparedBy) (string, error) {
	g.asked = append(g.asked, by)
	return "hil-1", nil
}

func (g *fakeGuard) Decision(context.Context, string) (string, error) {
	d := g.decisions[0]
	if len(g.decisions) > 1 {
		g.decisions = g.decisions[1:]
	}
	if d == "consumed" { // `browser submit --hil` sent it meanwhile
		g.drv.results = g.drv.query != ""
	}
	return d, nil
}

func (g *fakeGuard) Submit(_ context.Context, b blockedSubmit, id string) error {
	g.submitted = append(g.submitted, id)
	g.drv.results = g.drv.query != ""
	return nil
}

func (g *fakeGuard) TabID() int { return 77 }

// guardedDriver is a fakeDriver whose Go button (e3) submits a form, which
// an armed guard holds.
type guardedDriver struct {
	*fakeDriver
	g      *fakeGuard
	armed  bool
	closed int
}

func (d *guardedDriver) ArmSubmitGuard(context.Context) (submitGuard, error) {
	d.armed = true
	return d.g, nil
}

func (d *guardedDriver) Act(ctx context.Context, p *jevpick.PageState, a *jevpick.Action, text string) error {
	if a.ID != "e3" || !d.armed {
		return d.fakeDriver.Act(ctx, p, a, text)
	}
	d.executed = append(d.executed, "e3:")
	d.g.blocked = append(d.g.blocked, blockedSubmit{Seq: len(d.executed), URL: d.g.url,
		Form: browserctl.FormSummary{Action: d.g.url, Method: "get", Submitter: "#go"}})
	return nil
}

func gatedRun(t *testing.T, url string, decisions []string, timeout float64) (*guardedDriver, map[string]interface{}) {
	t.Helper()
	policyServer(t, nil)
	drv := &guardedDriver{fakeDriver: &fakeDriver{}}
	drv.g = &fakeGuard{url: url, decisions: decisions, drv: drv.fakeDriver}
	n := &Node{writer: zurich, open: func(context.Context, string) (driver, func(), error) {
		return drv, func() { drv.closed++ }, nil
	}}
	out, err := n.Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{
		"url": "https://shop.example.com/", "goal": "Search for zurich", "api_key": "k", "timeout": timeout,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return drv, out[0].Items[0].JSON
}

func TestUserRunIsNotGated(t *testing.T) {
	drv, res := gatedRun(t, "https://shop.example.com/search", []string{"rejected"}, 30)
	if res["status"] != "done" || drv.armed || len(drv.g.asked) != 0 || res["hil_id"] != nil {
		t.Fatalf("a run the user started must behave as before: armed=%v asked=%v res=%v", drv.armed, drv.g.asked, res)
	}
}

func TestAISubmitWaitsForApproval(t *testing.T) {
	t.Setenv(EnvAICaller, "chat")
	drv, res := gatedRun(t, "https://shop.example.com/search", []string{"pending", "pending", "approved"}, 30)
	if res["status"] != "done" || !drv.armed {
		t.Fatalf("status=%v (%v) armed=%v", res["status"], res["reason"], drv.armed)
	}
	if len(drv.g.asked) != 1 || drv.g.asked[0].AgentID != "chat" || strings.Join(drv.g.submitted, ",") != "hil-1" {
		t.Errorf("asked=%+v submitted=%v", drv.g.asked, drv.g.submitted)
	}
	if got := strings.Join(drv.executed, ","); got != "e1:zurich,e3:" || drv.closed != 1 {
		t.Errorf("executed=%s closed=%d", got, drv.closed)
	}
}

func TestAISubmitRejectedEndsBlocked(t *testing.T) {
	t.Setenv(EnvAICaller, "chat")
	drv, res := gatedRun(t, "https://shop.example.com/search", []string{"pending", "rejected"}, 30)
	if res["status"] != "blocked" || res["hil_id"] != "hil-1" || !strings.Contains(res["reason"].(string), "rejected") {
		t.Fatalf("res = %v", res)
	}
	if len(drv.g.submitted) != 0 || drv.closed != 1 || res["tab_id"] != nil {
		t.Errorf("submitted=%v closed=%d tab=%v", drv.g.submitted, drv.closed, res["tab_id"])
	}
}

func TestAISubmitStillPendingLeavesTheTabOpen(t *testing.T) {
	t.Setenv(EnvAICaller, "chat")
	drv, res := gatedRun(t, "https://shop.example.com/search", []string{"pending"}, 1)
	if res["status"] != "awaiting_approval" || res["hil_id"] != "hil-1" || res["tab_id"] != 77 {
		t.Fatalf("res = %v", res)
	}
	if !strings.Contains(res["reason"].(string), "browser submit --hil hil-1") || drv.closed != 0 || len(drv.g.submitted) != 0 {
		t.Errorf("reason=%v closed=%d submitted=%v", res["reason"], drv.closed, drv.g.submitted)
	}
}

func TestAISubmitConsumedElsewhereGoesOn(t *testing.T) {
	t.Setenv(EnvAICaller, "chat")
	drv, res := gatedRun(t, "https://shop.example.com/search", []string{"consumed"}, 30)
	if res["status"] != "done" || len(drv.g.asked) != 1 || len(drv.g.submitted) != 0 {
		t.Fatalf("a submit `browser submit --hil` already sent is not sent again: %v submitted=%v", res, drv.g.submitted)
	}
}

func TestAISubmitApprovedElsewhereIsNotSent(t *testing.T) {
	t.Setenv(EnvAICaller, "chat")
	drv, res := gatedRun(t, "https://shop.example.com/search", []string{"approved_elsewhere"}, 30)
	if res["status"] != "blocked" || !strings.Contains(res["reason"].(string), "approved outside") || len(drv.g.submitted) != 0 {
		t.Fatalf("res=%v submitted=%v", res, drv.g.submitted)
	}
}

func TestAISubmitOnADevPageNeedsNoApproval(t *testing.T) {
	t.Setenv(EnvAICaller, "chat")
	drv, res := gatedRun(t, "http://localhost:5173/search", []string{"rejected"}, 30)
	if res["status"] != "done" || len(drv.g.asked) != 0 || strings.Join(drv.g.submitted, ",") != "" || len(drv.g.submitted) != 1 {
		t.Fatalf("status=%v asked=%v submitted=%q", res["status"], drv.g.asked, drv.g.submitted)
	}
}

func TestAIRunFailsClosedWithoutAGuard(t *testing.T) {
	t.Setenv(EnvAICaller, "chat")
	policyServer(t, nil)
	_, err := testNode(&fakeDriver{}, zurich).Execute(context.Background(), workflow.NodeInput{},
		map[string]interface{}{"url": "https://shop.example.com/", "goal": "Search for zurich", "api_key": "k"})
	if err == nil || !strings.Contains(err.Error(), "chat started this run") {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("not a timeout")
	}
}
