package generate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/agentroster"
	"github.com/monoes/mono-agent/internal/monomind"
)

// RuntimeAuto picks the runtime from the validated roster.
const RuntimeAuto = "auto"

// WorkspacePurpose is the empty sandbox folder generation turns run in
// (~/.monoagent/workspaces/workflow-generate).
const WorkspacePurpose = "workflow-generate"

// TurnTimeout bounds one generation turn.
const TurnTimeout = 5 * time.Minute

// ErrNoRuntime is returned by PickRuntime when nothing can run a turn.
var ErrNoRuntime = fmt.Errorf("no AI runtime is set up: install and sign in to an agent CLI " +
	"(`monoagentcli agent scan` lists them, `monoagentcli agent install <runtime>` installs one), " +
	"then run `monoagentcli agent validate`, or pass --runtime <runtime>")

// PickRuntime chooses the runtime (and model) for runtime "auto": the first
// ready model of the validated roster on an installed runtime, else the
// first stale one, else the first installed runtime with its default model.
// scan may be nil (then roster entries are not checked against installs).
func PickRuntime(results []agentroster.Result, scan *monomind.ScanResult) (runtime, model string, err error) {
	roster := agentroster.Build(results, scan, time.Now(), agentroster.DefaultMaxAge)
	for _, want := range []string{agentroster.StateReady, agentroster.StateStale} {
		for _, rr := range roster {
			if scan != nil && !rr.Installed {
				continue
			}
			if rr.ExecutionSupported != nil && !*rr.ExecutionSupported {
				continue
			}
			for _, m := range rr.Models {
				if m.State != want {
					continue
				}
				if m.Model == agentroster.DefaultModel {
					return rr.Runtime, "", nil
				}
				return rr.Runtime, m.Model, nil
			}
		}
	}
	if scan != nil {
		for _, a := range scan.Installed() {
			if a.ExecutionSupported != nil && !*a.ExecutionSupported {
				continue
			}
			return a.ID, "", nil
		}
	}
	return "", "", ErrNoRuntime
}

// MonomindExec is the ExecFunc for a real turn: `monomind agent exec` on
// runtime with no tools (the answer is the document), in the turn sandbox
// every agent turn but coder mode asks for, in its own empty folder
// (~/.monoagent/workspaces/workflow-generate).
func MonomindExec(bin, runtime, model string) ExecFunc {
	return func(ctx context.Context, prompt, systemPrompt string) (string, error) {
		// An explicit folder even for a runtime that gets no sandbox, so the
		// turn never runs in (and can't read) the caller's directory.
		cwd, err := monomind.SandboxWorkspaceDir(WorkspacePurpose)
		if err != nil {
			return "", err
		}
		res, err := monomind.Exec(ctx, monomind.ExecOptions{
			Bin:          bin,
			Runtime:      runtime,
			Model:        model,
			Prompt:       prompt,
			SystemPrompt: systemPrompt,
			Cwd:          cwd,
			Timeout:      TurnTimeout,
			Sandbox:      monomind.TurnSandboxMode,
		}, nil)
		if err != nil {
			return "", fmt.Errorf("agent %s: %w", runtime, monomind.MarkNotSetup(err))
		}
		if res.Err != nil {
			monomind.ReclassifyMissingRuntime(ctx, runtime, res)
			return "", fmt.Errorf("agent %s turn failed: %w", runtime, monomind.MarkNotSetup(res.Err))
		}
		text := strings.TrimSpace(res.ResultText)
		if text == "" {
			return "", fmt.Errorf("agent %s returned an empty answer", runtime)
		}
		return text, nil
	}
}
