package health

import (
	"context"
	"strings"

	"github.com/monoes/mono-agent/internal/sandboxprobe"
)

// CheckSandbox reports, informationally, whether this process looks like it runs in an
// agent-mode sandbox. It never warns or fails: the probe is advisory (sandboxprobe).
const CheckSandbox = "core.sandbox"

// sandboxProbe is replaced by tests.
var sandboxProbe = func(dataDir string) sandboxprobe.Report {
	return sandboxprobe.Detect(sandboxprobe.Real(dataDir))
}

func checkSandbox(_ context.Context, env *Env) Result {
	r := sandboxProbe(env.DataDir)
	if !r.Sandboxed {
		return Result{Status: StatusInfo, Summary: "no sign of an agent-mode sandbox (advisory only)"}
	}
	return Result{
		Status:  StatusInfo,
		Summary: "this process looks sandboxed (advisory only)",
		Detail:  strings.Join(r.Signals, "\n"),
	}
}
