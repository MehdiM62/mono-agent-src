package operator

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/browserctl/browserctltest"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/workflow"
)

func asOperator(t *testing.T) {
	t.Helper()
	for _, k := range append(browserctl.AgentContextEnv(), workflow.EnvWorkflowRun, browserctl.EnvAICaller,
		browserctl.EnvAgentKind, browserctl.EnvAgentKindToken) {
		t.Setenv(k, "")
	}
}

func TestMarker(t *testing.T) {
	asOperator(t)
	if m := Marker(); m != "" || Refusal() != "" {
		t.Fatalf("the operator's terminal: %q", m)
	}
	for k, want := range map[string]string{
		workflow.EnvWorkflowRun:          "a workflow run",
		browserctl.EnvAICaller:           "the chat",
		hilsend.EnvAgentContext:          "an agent",
		orgsign.AgentContextMarkers()[0]: "an agent",
		"MONOAGENT_ACTOR":                "an agent",
	} {
		asOperator(t)
		t.Setenv(k, "x")
		if m := Marker(); m != k || !strings.Contains(Refusal(), k+" is set, so "+want) {
			t.Errorf("%s: marker %q, refusal %q", k, m, Refusal())
		}
	}
}

// What is not the operator's terminal is named for what it leaves on record.
func TestCaller(t *testing.T) {
	asOperator(t)
	if c := Caller(); c != "" {
		t.Fatalf("the operator's terminal: %q", c)
	}
	for _, c := range []struct{ env, value, want string }{
		{workflow.EnvWorkflowRun, "exec-7", "workflow:exec-7"},
		{browserctl.EnvAICaller, "chat", "chat"},
		{browserctl.EnvAICaller, "something", "agent:other"},
		{hilsend.EnvAgentContext, "1", "agent:other"},
		{orgsign.AgentContextMarkers()[0], "1", "agent:other"},
	} {
		asOperator(t)
		t.Setenv(c.env, c.value)
		if got := Caller(); got != c.want {
			t.Errorf("%s=%s: %q, want %q", c.env, c.value, got, c.want)
		}
	}
	// A shell that declares a kind it can't prove is other.
	asOperator(t)
	t.Setenv("CLAUDECODE", "1")
	t.Setenv(browserctl.EnvAgentKind, browserctl.KindCoder)
	if got := Caller(); got != "agent:other" {
		t.Errorf("an unproven coder shell: %q", got)
	}
	browserctltest.AsAgentKind(t, browserctl.KindCoder)
	if got := Caller(); got != "agent:coder" {
		t.Errorf("a coder chat's shell: %q", got)
	}
}

// A run an AI started, the org side's and any run in a process that is not
// the operator's are not the operator's; the user's own runs are.
func TestAIRun(t *testing.T) {
	asOperator(t)
	ctx := context.Background()
	if AIRun(ctx) || AIRun(workflow.WithTrigger(ctx, "trigger.manual", map[string]interface{}{"who": "me"})) {
		t.Fatal("the user's own run")
	}
	for name, run := range map[string]context.Context{
		"a chat's mark":   workflow.WithTrigger(ctx, "trigger.manual", map[string]interface{}{workflow.AICallerKey: "chat"}),
		"an agent's mark": workflow.WithTrigger(ctx, "trigger.manual", map[string]interface{}{workflow.AICallerKey: "other/org"}),
		"an org's call":   workflow.WithTrigger(ctx, workflow.TriggerTypeOrgTool, nil),
		"trigger.org":     workflow.WithTrigger(ctx, workflow.TriggerNodeTypeOrg, nil),
	} {
		if !AIRun(run) {
			t.Errorf("%s is the operator's", name)
		}
	}
	t.Setenv(workflow.EnvWorkflowRun, "exec-7")
	if !AIRun(ctx) {
		t.Error("a run in a process a workflow node started is the operator's")
	}
}
