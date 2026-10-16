package operator

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
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
