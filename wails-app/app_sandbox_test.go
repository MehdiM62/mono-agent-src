//go:build !windows

package main

import (
	"strings"
	"testing"
)

const sandboxFallbackJSON = `{"setting":"sandbox.fallback","value":"strictest","source":"saved","saved":"strictest","default":"run","values":["run","strictest","fail"],"env":"MONOAGENT_SANDBOX_FALLBACK","capable":true,"monomindVersion":"2.24.5"}`

func TestApp_SandboxFallback_ShellsOutAndReturnsTheCLIJSON(t *testing.T) {
	bin, argsLog := chatFakeCLI(t,
		fakeChatReply{match: "sandbox fallback", stdout: sandboxFallbackJSON + "\n"},
	)
	a, _ := newCLIChatApp(t, bin)
	a.setActiveProfileID("work")

	if got := a.SandboxFallbackGet(); got != sandboxFallbackJSON {
		t.Errorf("SandboxFallbackGet = %s", got)
	}
	if got := a.SandboxFallbackSet("strictest"); got != sandboxFallbackJSON {
		t.Errorf("SandboxFallbackSet = %s", got)
	}
	// A value that looks like a flag stays the value of --set.
	a.SandboxFallbackSet("--json")

	var calls []string
	for _, line := range readArgsLog(t, argsLog) {
		calls = append(calls, strings.TrimPrefix(line, "--profile work --json "))
	}
	want := "sandbox fallback|sandbox fallback --set=strictest|sandbox fallback --set=--json"
	if strings.Join(calls, "|") != want {
		t.Errorf("CLI calls = %q, want %q", calls, want)
	}
}
