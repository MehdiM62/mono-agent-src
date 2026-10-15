package main

import (
	"testing"

	"github.com/monoes/mono-agent/internal/account"
)

func TestSecurityDocStatesWhatLeavesTheMachine(t *testing.T) {
	assertNoStaleClaims(t, "SECURITY.md")
	assertSays(t, "SECURITY.md", "## Network use, telemetry and crash reporting", "No analytics, no usage counters, no device identifier",
		"A monoes.me sign-in has been required since "+account.EnforceDate().UTC().Format("2006-01-02"), "the email address you type", "IP address", "client id `"+account.ClientID+"`", "refresh token",
		"GitHub", "update", graceText(), "~/.monoagent/account/", "session.json", "refresh.enc", "session.lock",
		"The sign-in requirement is a product check", "not a security boundary", "The monoes.me session", "monoes..account",
		"### One sign-in per machine, never copied", "never copy or restore", "signs out the original and every copy",
		"### When a sign-in is locked", "does not sign out an install", "The account gate that uses this session is **live**")
}
