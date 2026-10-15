package health

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/monomind"
)

// Without --profile the machine checks still run and every per-profile check
// reports that a profile must be named; none of them guesses one.
func TestPerProfileChecksAskForAProfile(t *testing.T) {
	ctx := context.Background()
	called := false
	env := &Env{
		ProfileRoot: func(string) string { called = true; return t.TempDir() },
		VaultState:  func(context.Context, string) (string, error) { called = true; return "ok", nil },
		JevKey:      func(context.Context) (string, error) { called = true; return "env", nil },
		JevModels:   func(context.Context) error { called = true; return nil },
		MonomindHandshake: func(context.Context) (*monomind.VersionInfo, error) {
			return &monomind.VersionInfo{V: 1, Version: "1.0.0"}, nil
		},
	}
	results := map[string]Result{
		"profile":  checkProfile(ctx, env),
		"vault":    checkVault(ctx, env),
		"jev key":  checkJevKey(ctx, env),
		"jev api":  checkJevAPI(ctx, env),
		"monomind": checkMonomindProfileInit(ctx, env),
		"mcp pin":  checkMonomindMCPPin(ctx, env),
	}
	for name, res := range results {
		if res.Status != StatusSkip || !strings.Contains(res.Summary, "pass --profile") {
			t.Errorf("%s: %+v, want a skip asking for --profile", name, res)
		}
	}
	if called {
		t.Error("a per-profile check ran against some profile although none was named")
	}
	fixEnv := &Env{EnsureProfile: func(string) error { called = true; return nil }}
	if err := fixProfileLayout(ctx, fixEnv, func(string) {}); err == nil || called {
		t.Errorf("profile layout fix without a profile: err %v, ran %v", err, called)
	}
}
