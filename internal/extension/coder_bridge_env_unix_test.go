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
// writes its environment to envOut, is admitted and finishes.
func fakeCLIScript(t *testing.T) (bin, envOut string) {
	t.Helper()
	dir := t.TempDir()
	bin, envOut = filepath.Join(dir, "monoagentcli"), filepath.Join(dir, "env.txt")
	script := "#!/bin/sh\n" +
		`case "$*" in` + "\n" +
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
	for _, k := range coderAgentEnv() {
		if strings.Contains(env, "\n"+k+"=") || strings.HasPrefix(env, k+"=") {
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

// Only a turn is stripped: what else a coder.* request runs, and the side
// panel's chat (chat.send, with --tools), keep the bridge's env.
func TestOnlyCoderTurnsAreStripped(t *testing.T) {
	withRunMarkers(t)
	bin, envOut := fakeCLIScript(t)
	if _, err := (&cliCoder{chat: cliChatBackend{bin: bin}}).Run(context.Background(), "", "coder", "queue", "list"); err == nil {
		t.Fatal("the stand-in should refuse")
	}
	if env := envFile(t, envOut); !strings.Contains(env, "MONOAGENT_WORKFLOW_RUN=exec-1") || !strings.Contains(env, "CLAUDECODE=1") {
		t.Errorf("coder.* run lost the bridge's markers:\n%s", env)
	}

	_ = os.Remove(envOut)
	err := (&cliChatBackend{bin: bin}).RunTurn(context.Background(), ChatTurnSpec{Conversation: "c2", Turn: "t2", Instance: "ext-1", Message: "hi"}, func(ChatEvent) {})
	if err != nil {
		t.Fatalf("chat.send turn: %v", err)
	}
	env := envFile(t, envOut)
	for _, want := range []string{"MONOAGENT_WORKFLOW_RUN=exec-1", "MONOAGENT_AI_CALLER=chat", "CLAUDECODE=1"} {
		if !strings.Contains(env, want) {
			t.Errorf("chat.send's child lost %s", want)
		}
	}
}
