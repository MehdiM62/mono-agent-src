//go:build !windows

package extension

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeCLIScript writes a stand-in for monoagentcli: `coder list` says the
// conversation was started in the desktop app, a turn (`chat --conversation`)
// writes its environment to envOut, is admitted and finishes. `coder queue
// add` refuses an agent the way the real one does (any variable of
// browserctl.AgentContextEnv, which coderAgentEnv is held equal to, set) and
// otherwise queues; anything else writes its environment and refuses.
func fakeCLIScript(t *testing.T) (bin, envOut string) {
	t.Helper()
	dir := t.TempDir()
	bin, envOut = filepath.Join(dir, "monoagentcli"), filepath.Join(dir, "env.txt")
	script := "#!/bin/sh\n" +
		`case "$*" in` + "\n" +
		`  *"coder queue add"*) for k in ` + strings.Join(coderAgentEnv(), " ") + `; do eval "v=\${$k}"; ` +
		`if [ -n "$v" ]; then echo "{\"error\":\"queueing a coder chat is refused inside an agent ($k is set)\",\"code\":\"agent_context\"}"; exit 3; fi; done; ` +
		`echo '{"id":"20261015-120000-abcdef","kind":"tokens"}';;` + "\n" +
		`  *"coder list"*) echo '{"conversations":[{"id":"c1","cwd":"/w","startedInDesktop":true,"running":false}]}';;` + "\n" +
		`  *"chat --conversation"*|*"chat --profile"*) env > '` + envOut + `'; echo '{"admitted":true,"turn":{"id":"t"}}'; echo '{"seq":1,"type":"turn.finished","payload":{"status":"completed"}}';;` + "\n" +
		`  *) env > '` + envOut + `'; echo '{"error":"refused for the test"}'; exit 3;;` + "\n" +
		"esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, envOut
}

// runMarkers are a bridge started from a workflow run the chat made, inside
// an agent's shell.
func withRunMarkers(t *testing.T) {
	t.Setenv("MONOAGENT_WORKFLOW_RUN", "exec-1")
	t.Setenv("MONOAGENT_AI_CALLER", "chat")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("MONOAGENT_TEST_KEEP", "kept")
}

func envFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			return string(b)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child never wrote %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A side-panel follow-up runs as the person's turn: no agent or run marker
// of the bridge's reaches it, and the bridge's own env is untouched.
func TestCoderSendTurnRunsWithoutMarkers(t *testing.T) {
	withRunMarkers(t)
	bin, envOut := fakeCLIScript(t)
	srv := NewServer("127.0.0.1:0", zerolog.Nop())
	registerCoderBridge(srv, &cliCoder{chat: cliChatBackend{bin: bin}})
	h, _ := srv.handlerFor(MethodCoderSend)
	if _, err := h(context.Background(), &Request{Method: MethodCoderSend, Params: map[string]any{"conversationId": "c1", "text": "and the footer", "profile": "default"}},
		func(string, string) {}); err != nil {
		t.Fatalf("coder.send: %v", err)
	}
	env := envFile(t, envOut)
	for _, k := range coderTurnEnv() {
		if hasVar(env, k) {
			t.Errorf("the turn's env has %s", k)
		}
	}
	if !strings.Contains(env, "MONOAGENT_TEST_KEEP=kept") {
		t.Error("the turn lost the rest of the env")
	}
	if os.Getenv("MONOAGENT_WORKFLOW_RUN") != "exec-1" || os.Getenv("MONOAGENT_AI_CALLER") != "chat" {
		t.Error("the bridge's own env changed")
	}
}

func hasVar(env, k string) bool {
	return strings.HasPrefix(env, k+"=") || strings.Contains(env, "\n"+k+"=")
}

// The other commands a coder.* request runs lose the agent markers only (as
// before #831), and the side panel's chat (chat.send, with --tools) keeps
// the bridge's env whole.
func TestOnlyCoderTurnsLoseTheRunMarkers(t *testing.T) {
	withRunMarkers(t)
	bin, envOut := fakeCLIScript(t)
	if _, err := (&cliCoder{chat: cliChatBackend{bin: bin}}).Run(context.Background(), "", "coder", "projects", "list"); err == nil {
		t.Fatal("the stand-in should refuse")
	}
	env := envFile(t, envOut)
	for _, k := range coderAgentEnv() {
		if hasVar(env, k) {
			t.Errorf("a coder.* command kept the agent marker %s", k)
		}
	}
	for _, want := range []string{"MONOAGENT_WORKFLOW_RUN=exec-1", "MONOAGENT_AI_CALLER=chat"} {
		if !strings.Contains(env, want) {
			t.Errorf("a coder.* command lost %s", want)
		}
	}

	_ = os.Remove(envOut)
	err := (&cliChatBackend{bin: bin}).RunTurn(context.Background(), ChatTurnSpec{Conversation: "c2", Turn: "t2", Instance: "ext-1", Message: "hi"}, func(ChatEvent) {})
	if err != nil {
		t.Fatalf("chat.send turn: %v", err)
	}
	env = envFile(t, envOut)
	for _, want := range []string{"MONOAGENT_WORKFLOW_RUN=exec-1", "MONOAGENT_AI_CALLER=chat", "CLAUDECODE=1"} {
		if !strings.Contains(env, want) {
			t.Errorf("chat.send's child lost %s", want)
		}
	}
}

// A bridge in an agent's shell still queues the person's coder chats:
// `coder queue add` never sees the bridge's agent markers.
func TestCoderQueueFromABridgeInAnAgentShell(t *testing.T) {
	withRunMarkers(t)
	bin, _ := fakeCLIScript(t)
	srv := NewServer("127.0.0.1:0", zerolog.Nop())
	registerCoderBridge(srv, &cliCoder{chat: cliChatBackend{bin: bin}})
	h, _ := srv.handlerFor(MethodCoderQueue)
	res, err := h(context.Background(), &Request{Method: MethodCoderQueue,
		Params: map[string]any{"kind": "tokens", "tabId": float64(5), "profile": "default"}}, func(string, string) {})
	if err != nil {
		t.Fatalf("coder.queue with CLAUDECODE in the bridge env: %v", err)
	}
	if item, _ := res.(map[string]any)["item"].(map[string]any); item["id"] != "20261015-120000-abcdef" {
		t.Errorf("reply = %v", res)
	}

	// The stand-in does refuse an agent: the same request with the markers
	// reaching it would fail.
	if out, err := (&cliChatBackend{bin: bin}).run(context.Background(), "coder", "queue", "add"); err == nil {
		t.Errorf("the stand-in queued with CLAUDECODE set: %s", out)
	}
}
