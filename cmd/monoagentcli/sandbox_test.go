package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/monomind"
)

func withFallbackCaps(t *testing.T, caps ...string) {
	t.Helper()
	old := capabilityProbe
	capabilityProbe = func(*cobra.Command) (*monomind.CapabilitySet, error) {
		return monomind.NewCapabilitySet("2.24.5", caps...), nil
	}
	t.Cleanup(func() { capabilityProbe = old })
}

func runSandboxCLI(t *testing.T, dbPath string, args ...string) (sandboxFallbackReport, int, string) {
	t.Helper()
	cmd := newSandboxCmd(&globalConfig{DBPath: dbPath, ProfileID: "default", JSONOutput: true})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(append([]string{"fallback"}, args...))
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	err := cmd.Execute()
	var rep sandboxFallbackReport
	_ = json.Unmarshal(out.Bytes(), &rep)
	return rep, exitCodeFor(err), out.String()
}

func TestSandboxFallbackCLI(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	t.Setenv(monomind.SandboxFallbackEnv, "")
	withFallbackCaps(t, monomind.CapAgentExecSandboxFallback)

	rep, code, out := runSandboxCLI(t, dbPath)
	if code != 0 || rep.Value != "run" || rep.Source != "default" || !rep.Capable || rep.Setting != "sandbox.fallback" {
		t.Fatalf("default: exit %d %s", code, out)
	}
	for _, v := range []string{"strictest", "fail", "run"} {
		rep, code, out = runSandboxCLI(t, dbPath, "--set", v)
		if code != 0 || rep.Value != v || rep.Source != "saved" {
			t.Fatalf("--set %s: exit %d %s", v, code, out)
		}
	}
	if _, code, out = runSandboxCLI(t, dbPath, "--set", "strictest"); code != 0 {
		t.Fatal(out)
	}
	// An invalid value is a validation error (3) that changes nothing.
	if _, code, out = runSandboxCLI(t, dbPath, "--set", "loose"); code != 3 {
		t.Fatalf("invalid value: exit %d %s", code, out)
	}
	if rep, _, _ = runSandboxCLI(t, dbPath); rep.Value != "strictest" {
		t.Fatalf("an invalid --set must not change the saved value: %+v", rep)
	}
	// The environment wins over the saved value and says so.
	t.Setenv(monomind.SandboxFallbackEnv, "fail")
	if rep, _, _ = runSandboxCLI(t, dbPath); rep.Value != "fail" || rep.Source != "env" || rep.Saved != "strictest" {
		t.Fatalf("env override: %+v", rep)
	}
}

func TestSandboxFallbackCLIWithoutCapability(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	t.Setenv(monomind.SandboxFallbackEnv, "")
	withFallbackCaps(t) // an older monomind
	if rep, code, out := runSandboxCLI(t, dbPath); code != 0 || rep.Capable || rep.MonomindVersion != "2.24.5" {
		t.Fatalf("exit %d %s", code, out)
	}
}

// The provider reads the database on each turn: a `sandbox fallback --set`
// from another process (the app) reaches a running daemon within the TTL,
// and a missing or unreadable database is the default, not an error.
func TestSandboxFallbackProviderSeesChanges(t *testing.T) {
	dbPath := newChatCLITestDB(t)
	withFallbackCaps(t)
	t.Setenv(monomind.SandboxFallbackEnv, "")
	prev := monomind.SandboxFallbackProvider
	t.Cleanup(func() { monomind.SandboxFallbackProvider = prev })

	cfg := &globalConfig{DBPath: dbPath}
	installSandboxFallbackProvider(cfg)
	if got := monomind.SandboxFallbackSetting(); got != "run" {
		t.Fatalf("unset = %q", got)
	}
	runSandboxCLI(t, dbPath, "--set", "strictest")
	time.Sleep(sandboxFallbackTTL + 100*time.Millisecond)
	if got := monomind.SandboxFallbackSetting(); got != "strictest" {
		t.Fatalf("after set = %q, want strictest", got)
	}
	cfg.DBPath = dbPath + ".missing"
	time.Sleep(sandboxFallbackTTL + 100*time.Millisecond)
	if got := monomind.SandboxFallbackSetting(); got != "run" {
		t.Fatalf("missing database = %q, want run", got)
	}
}
