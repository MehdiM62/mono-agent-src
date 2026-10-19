package control

import (
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A chat run that calls a sub-workflow pauses for it (WAITING, with no
// person involved); the engine resumes it when the child ends. The chat is
// watched through that pause and gets the final answer. It used to stop
// watching at the pause, so the answer never reached the chat.
func TestChat_LastNodeAnswersThroughASubWorkflow(t *testing.T) {
	f, _ := chatFlow(t, "delegate", "lastNode", execNode(map[string]interface{}{"workflowId": "chat-child"}))
	// Slower than a poll of the watcher, so it sees the parent waiting.
	f.chain(t, "chat-child", workflow.TriggerTypeExecuteWorkflow, true, workflow.WorkflowNode{Type: "core.code",
		Config: map[string]interface{}{"code": `const until = Date.now() + 600; while (Date.now() < until) {}
return [{ json: { output: "child: " + $json.chatInput } }]`}})
	seen := 0
	session := chatSession("delegate")
	say(t, f, "delegate", session, "hello")
	if m := botSays(t, session, &seen); m.Text != "child: hello" {
		t.Fatalf("bot = %q", m.Text)
	}
}
