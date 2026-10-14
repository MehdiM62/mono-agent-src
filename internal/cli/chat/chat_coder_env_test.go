package chat

import (
	"testing"

	"github.com/monoes/mono-agent/internal/hilsend"
)

func TestAgentSendEnvMarksTheAgentAndItsChat(t *testing.T) {
	j := &turnJournal{conversationID: "conv-1"}
	env := agentSendEnv(j, "lead", "")
	if env[hilsend.EnvAgentContext] == "" || env[hilsend.EnvConversation] != "conv-1" || env[hilsend.EnvAgentRole] != "lead" {
		t.Fatalf("env = %v", env)
	}
	if _, ok := agentSendEnv(nil, "x", "")[hilsend.EnvConversation]; ok {
		t.Fatal("a nil journal must not set a conversation")
	}
}
