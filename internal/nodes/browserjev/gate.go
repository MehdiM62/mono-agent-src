package browserjev

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/workflow"
)

// EnvAICaller marks a process whose workflow run an AI caller started
// without the process being an agent's own: the chat's run_workflow tool
// sets it to "chat" on the `workflow run` it starts (internal/ai/chat).
const EnvAICaller = "MONOAGENT_AI_CALLER"

// caller is who started a run when it was not the user.
type caller struct {
	name string             // for the run's errors: "chat", "an agent (CLAUDECODE)", "org growth"
	by   hilsend.PreparedBy // for the approval card
}

// aiCaller reports who started this run when it was not the user: a chat
// tool call (EnvAICaller), an agent's process (a coder chat, an org role's
// shell, a coding agent's shell or MCP server: browserctl.IsAgentContext,
// the test `monoagentcli browser` holds agents to), a run either of them
// started with `workflow run` (workflow.AICallerKey), or the org side (a granted call, a message to an
// automation role, a trigger.org event). A run the user started from the
// app, a terminal, a schedule or a webhook is none of these.
func aiCaller(ctx context.Context) (caller, bool) {
	if c, ok := envCaller(); ok {
		return c, true
	}
	// A run this process didn't start: the AI caller's `workflow run` marked
	// it (MarkAICaller).
	if v, _ := workflow.TriggerDataFrom(ctx)[workflow.AICallerKey].(string); strings.TrimSpace(v) != "" {
		return caller{name: v, by: hilsend.PreparedBy{AgentID: v}}, true
	}
	switch workflow.TriggerTypeFrom(ctx) {
	case workflow.TriggerTypeOrgTool, workflow.TriggerTypeOrgMessage, workflow.TriggerNodeTypeOrg:
		src := workflow.PublicationSource(ctx, workflow.NodeInput{})
		return caller{name: strings.TrimSpace("org " + src.OrgID),
			by: hilsend.PreparedBy{Org: src.OrgID, Role: src.RoleID, AgentID: src.AgentID}}, true
	}
	return caller{}, false
}

// envCaller is an AI caller this process's environment names.
func envCaller() (caller, bool) {
	if v := strings.TrimSpace(os.Getenv(EnvAICaller)); v != "" {
		return caller{name: v, by: hilsend.PreparedBy{AgentID: v}}, true
	}
	if browserctl.IsAgentContext() {
		name := "an agent"
		if m := orgsign.AgentContextMarker(); m != "" {
			name += " (" + m + ")"
		}
		return caller{name: name, by: browserctl.RequestedBy()}, true
	}
	return caller{}, false
}

// MarkAICaller marks a run's trigger data when an AI caller starts it from
// this process (`workflow run`), so the engine that runs it — this one, or a
// daemon that adopts a --no-wait run or resumes a paused one — gates it too.
func MarkAICaller(data map[string]interface{}) {
	if c, ok := envCaller(); ok && data != nil {
		data[workflow.AICallerKey] = c.name
	}
}

// blockedSubmit is one submit the guard stopped (C1 submit_guard's blocked[]).
type blockedSubmit struct {
	Seq  int
	URL  string
	Form browserctl.FormSummary // sensitive values masked
}

// submitGuard is the submit guard armed on the run's tab and the approval
// behind it (gate_ext.go).
type submitGuard interface {
	// Blocked returns the submits it blocked since the last call.
	Blocked(ctx context.Context) ([]blockedSubmit, error)
	// Ask makes the browser.submit Human-in-Loop item for b.
	Ask(ctx context.Context, b blockedSubmit, by hilsend.PreparedBy) (hilID string, err error)
	// Decision is the item's status: pending, approved, rejected,
	// consumed (`browser submit --hil` already sent it), or
	// approved_elsewhere (not by the user, so it can't be used).
	Decision(ctx context.Context, hilID string) (string, error)
	// Submit lets b through, exactly once, then the guard holds again; with
	// hilID, the approved item is used up by it.
	Submit(ctx context.Context, b blockedSubmit, hilID string) error
	// TabID is the tab it guards (0 when the page can't say).
	TabID() int
}

// guardable is a driver that can arm the submit guard on its tab.
type guardable interface {
	ArmSubmitGuard(ctx context.Context) (submitGuard, error)
}

// approvalPoll is how often a waiting run reads its item's decision.
var approvalPoll = 2 * time.Second

// guardSettle is how long a submit an action set off takes to reach the
// guard's record (the page posts it to the extension).
var guardSettle = 300 * time.Millisecond

func waitForGuard(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(guardSettle):
	}
}

// gateEnd is how the gate ended a run: its status and reason, and the item.
type gateEnd struct {
	status, reason, hilID string
}

// settleSubmits handles what the guard blocked during the last action. A
// submit on a local or dev page goes through (an allowlisted real site
// still asks: the allowlist says where an agent may go, not what it may
// send). One on a real site becomes a browser.submit item and the run
// waits for the user: approved, it goes through and the run goes on (nil);
// rejected, the run ends blocked; still pending when the run's time is up,
// it ends awaiting_approval and the tab stays open, so `browser submit
// --hil <id>` can still send it.
func settleSubmits(ctx context.Context, g submitGuard, c caller) (*gateEnd, error) {
	blocked, err := g.Blocked(ctx)
	if err != nil || len(blocked) == 0 {
		return nil, err
	}
	b := blocked[len(blocked)-1] // one action, one submit: the latest form state
	origin := browserctl.Origin(b.URL)
	if browserctl.IsLocal(b.URL) {
		return nil, g.Submit(ctx, b, "")
	}
	id, err := g.Ask(ctx, b, c.by)
	if err != nil {
		return nil, fmt.Errorf("ask for approval of the submit on %s: %w", origin, err)
	}
	for {
		status, err := g.Decision(ctx, id)
		if err != nil && ctx.Err() == nil {
			return nil, err
		}
		switch status {
		case "approved":
			return nil, g.Submit(ctx, b, id)
		case "consumed":
			return nil, nil
		case "rejected":
			return &gateEnd{"blocked", fmt.Sprintf("the user rejected the form submit on %s (Human-in-Loop item %s)", origin, id), id}, nil
		case "approved_elsewhere":
			return &gateEnd{"blocked", fmt.Sprintf("the form submit on %s (Human-in-Loop item %s) was approved outside "+
				"`monoagentcli hil approve` and the Human in Loop page, so it was not sent", origin, id), id}, nil
		}
		select {
		case <-ctx.Done():
			return &gateEnd{"awaiting_approval", fmt.Sprintf("the form submit on %s waits for the user's approval (Human-in-Loop item %s); "+
				"once approved, `monoagentcli browser submit --hil %s` sends it from the open tab", origin, id, id), id}, nil
		case <-time.After(approvalPoll):
		}
	}
}
