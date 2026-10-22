package browserctltest

import (
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
)

// AsAgentKind gives the test the environment a spawner gives an agent of
// kind (browserctl.KindCoder, KindApply or KindOrg): the declared kind and the token
// browserctl.IssueAgentKind issues for it, its record under a temporary
// HOME so a test never writes to the user's. The caller sets the agent
// marker itself (CLAUDECODE, MONOAGENT_AGENT_CONTEXT, ...), as a spawner
// does.
func AsAgentKind(t testing.TB, kind string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	env, _, done := browserctl.IssueAgentKind(kind)
	t.Cleanup(done)
	if len(env) != 2 {
		t.Fatalf("no token issued for kind %q: %q", kind, env)
	}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
}

// AsApplyTask is AsAgentKind for the apply kind of AI task taskID
// (browserctl.IssueApplyKind): the token's record names the task.
func AsApplyTask(t testing.TB, taskID string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	env, _, done := browserctl.IssueApplyKind(taskID)
	t.Cleanup(done)
	if len(env) != 2 {
		t.Fatalf("no token issued for the apply kind: %q", env)
	}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
}
