// Package operator tells the operator's own terminal from everything else
// that runs mono-agent's CLI: an agent, a process of any kind of agent, the
// chat's runs and whatever a workflow node starts. What only the operator
// may decide (approvals, browser permissions, org signatures, an org role's
// full access) is refused unless Marker is "".
package operator

import (
	"context"
	"os"
	"strings"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Marker names the environment variable that makes this process not the
// operator's own terminal, or "" for the operator: a process a workflow node
// started (workflow.EnvWorkflowRun, which a chat or an org can have built),
// a chat tool call's run (browserctl.EnvAICaller), an agent (hilsend.Detect)
// or any kind of agent (browserctl.AgentKind).
func Marker() string {
	for _, k := range []string{workflow.EnvWorkflowRun, browserctl.EnvAICaller} {
		if os.Getenv(k) != "" {
			return k
		}
	}
	if _, agent := hilsend.Detect(); !agent && browserctl.AgentKind() == "" {
		return ""
	}
	if m := orgsign.AgentContextMarker(); m != "" {
		return m
	}
	for _, k := range []string{hilsend.EnvAgentContext, "MONOAGENT_ACTOR", browserctl.EnvAgentKind} {
		if os.Getenv(k) != "" {
			return k
		}
	}
	return "an agent marker"
}

// Caller names what runs this process when it is not the operator's own
// terminal, for what it leaves on record (the author of a task it adds):
// "workflow:<execution id>" for a process a workflow node started, "chat" for
// the chat's runs, "agent:<kind>" for an agent (browserctl.AgentKind: a kind it
// could not prove is other); "" for the operator.
func Caller() string {
	switch m := Marker(); m {
	case "":
		return ""
	case workflow.EnvWorkflowRun:
		return "workflow:" + strings.TrimSpace(os.Getenv(m))
	case browserctl.EnvAICaller:
		if strings.TrimSpace(os.Getenv(m)) == browserctl.KindChat {
			return browserctl.KindChat
		}
	}
	kind := browserctl.AgentKind()
	if kind == "" || kind == browserctl.KindChat {
		kind = browserctl.KindOther
	}
	return "agent:" + kind
}

// AIRun reports whether the workflow run in ctx is not the operator's: this
// process is not the operator's own terminal (Marker), an AI started the run
// (workflow.AICallerKey, the mark a chat's or an agent's `workflow run`
// leaves for the engine that runs it: this one, or a daemon that adopts it),
// or the org side did (a granted call, a message to an automation role, a
// trigger.org event). A run the user starts from the app, a terminal, a
// schedule or a webhook is the operator's.
func AIRun(ctx context.Context) bool {
	if Marker() != "" {
		return true
	}
	if v, _ := workflow.TriggerDataFrom(ctx)[workflow.AICallerKey].(string); strings.TrimSpace(v) != "" {
		return true
	}
	switch workflow.TriggerTypeFrom(ctx) {
	case workflow.TriggerTypeOrgTool, workflow.TriggerTypeOrgMessage, workflow.TriggerNodeTypeOrg:
		return true
	}
	return false
}

// Refusal says why this process is not the operator's own terminal, for an
// error message, or "" for the operator.
func Refusal() string {
	switch m := Marker(); m {
	case "":
		return ""
	case workflow.EnvWorkflowRun:
		return m + " is set, so a workflow run is running this command"
	case browserctl.EnvAICaller:
		return m + " is set, so the chat is running this command"
	default:
		return m + " is set, so an agent is running this command"
	}
}
