package browserctl

import (
	"os"
	"strings"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
)

// EnvCoderTitle names the coder conversation driving the browser; it labels
// the agent's tab group.
const EnvCoderTitle = "MONOAGENT_CODER_TITLE"

// IsAgentContext reports whether an agent runs this process: any coding
// agent's or org role's env marker (orgsign.AgentContextMarker), or the
// markers a monoagent coder turn sets (hilsend.Detect, the rule `hil`
// already uses to refuse an agent).
func IsAgentContext() bool {
	_, agent := hilsend.Detect()
	return agent
}

// AgentContextEnv lists every variable IsAgentContext reads, so a test can
// clear them to act as the operator's own terminal.
func AgentContextEnv() []string {
	return append(orgsign.AgentContextMarkers(), hilsend.EnvAgentContext, "MONOAGENT_ACTOR")
}

// RequestedBy says which agent asked, for a HIL item's card.
func RequestedBy() hilsend.PreparedBy {
	who, _ := hilsend.Detect()
	return who
}

// ContextKey names this caller's session state file: the coder
// conversation, else the org role, else "agent" for any other agent, else
// "human".
func ContextKey() string {
	if c := strings.TrimSpace(os.Getenv(hilsend.EnvConversation)); c != "" {
		return "conv-" + safeKey(c)
	}
	who, agent := hilsend.Detect()
	if !agent {
		return "human"
	}
	if who.Role != "" {
		if who.Org != "" {
			return "role-" + safeKey(who.Org) + "-" + safeKey(who.Role)
		}
		return "role-" + safeKey(who.Role)
	}
	return "agent"
}

// GroupTitle is the tab group an agent's tabs go into: "Coder · <label>",
// the label being the coder conversation's title, else the role, else
// "Agent".
func GroupTitle() string {
	label := strings.TrimSpace(os.Getenv(EnvCoderTitle))
	if label == "" {
		label = RequestedBy().Role
	}
	if label == "" {
		label = "Agent"
	}
	if r := []rune(label); len(r) > 40 {
		label = string(r[:40]) + "…"
	}
	return "Coder · " + label
}

// safeKey keeps a file-name-safe, bounded part of s.
func safeKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
		if b.Len() >= 64 {
			break
		}
	}
	out := strings.Trim(b.String(), ".")
	if out == "" {
		return "_"
	}
	return out
}
