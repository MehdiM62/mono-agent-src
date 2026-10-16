package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/monomind"
)

// The binary vouches for the org kind of the role shells its org runs start.
func TestOrgLaunchesIssueAnOrgKindToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if monomind.OrgKindEnv == nil {
		t.Fatal("no org kind hook installed")
	}
	env, done := monomind.OrgKindEnv()
	if len(env) != 2 || env[0] != browserctl.EnvAgentKind+"=org" || !strings.HasPrefix(env[1], browserctl.EnvAgentKindToken+"=") {
		t.Fatalf("env = %q", env)
	}
	record := filepath.Join(home, ".monoagent", "agent-kinds", strings.TrimPrefix(env[1], browserctl.EnvAgentKindToken+"=")+".json")
	if _, err := os.Stat(record); err != nil {
		t.Fatalf("no record: %v", err)
	}
	done()
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Errorf("the record outlives the run: %v", err)
	}
}
