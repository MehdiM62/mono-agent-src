package health

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/sandboxprobe"
)

func TestSandboxCheckIsInformationalOnly(t *testing.T) {
	old := sandboxProbe
	defer func() { sandboxProbe = old }()
	for _, sandboxed := range []bool{false, true} {
		sandboxProbe = func(string) sandboxprobe.Report {
			return sandboxprobe.Report{Sandboxed: sandboxed, Signals: []string{"MONOAGENT_SANDBOX=bwrap is set"}}
		}
		res := checkSandbox(context.Background(), &Env{DataDir: "/x"})
		if res.Status != StatusInfo || !strings.Contains(res.Summary, "advisory") {
			t.Errorf("sandboxed=%v: %+v", sandboxed, res)
		}
		if sandboxed != strings.Contains(res.Summary, "looks sandboxed") {
			t.Errorf("sandboxed=%v: %q", sandboxed, res.Summary)
		}
	}
	var registered bool
	for _, c := range Default().Checks() {
		registered = registered || c.ID == CheckSandbox
	}
	if !registered {
		t.Fatal("sandbox check is not registered")
	}
}
