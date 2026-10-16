package extension

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/monoes/mono-agent/internal/hilsend"
	"github.com/monoes/mono-agent/internal/orgsign"
)

// cliCoder is the real coderCLI: this binary. A coder turn it starts
// (Start: coder.send, which only runs after the desktop-started check) runs
// without any variable in coderAgentEnv: the person typed it in their
// browser, so a bridge that happens to run under an agent's shell, a chat or
// a workflow run must not make it an agent's or a run's turn (the coder-turn
// gate refuses those). Everything else it runs (Run), and the bridge itself,
// keep their environment.
type cliCoder struct{ chat cliChatBackend }

// coderAgentEnv is operator.MarkerEnv, which this package can't import
// (operator imports it): every variable that makes a process not the
// operator's own terminal. A test holds the two lists equal.
func coderAgentEnv() []string {
	return append(orgsign.AgentContextMarkers(), hilsend.EnvAgentContext, "MONOAGENT_ACTOR",
		"MONOAGENT_WORKFLOW_RUN", "MONOAGENT_AI_CALLER", "MONOAGENT_AGENT_KIND", "MONOAGENT_AGENT_KIND_TOKEN")
}

// withoutCoderAgentEnv is env minus coderAgentEnv.
func withoutCoderAgentEnv(env []string) []string {
	markers := coderAgentEnv()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); !slices.Contains(markers, k) {
			out = append(out, kv)
		}
	}
	return out
}

// command builds the child; a turn's runs without coderAgentEnv.
func (c *cliCoder) command(ctx context.Context, stdin string, args []string, turn bool) (*exec.Cmd, error) {
	bin, err := c.chat.binary()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = monomindWaitDelay
	cmd.Stdin = strings.NewReader(stdin)
	if turn {
		cmd.Env = withoutCoderAgentEnv(os.Environ())
	}
	return cmd, nil
}

func (c *cliCoder) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd, err := c.command(ctx, stdin, args, false)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, &RequestError{Code: CodeTimeout, Err: ctx.Err()}
		}
		return nil, cliFailure(stdout.Bytes(), stderr.String(), err)
	}
	return stdout.Bytes(), nil
}

type execProc struct {
	cmd *exec.Cmd
	out io.Reader
}

func (p *execProc) Stdout() io.Reader { return p.out }
func (p *execProc) Wait() error       { return p.cmd.Wait() }

func (c *cliCoder) Start(ctx context.Context, stdin string, args ...string) (coderProc, error) {
	cmd, err := c.command(ctx, stdin, args, true)
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start monoagentcli: %w", err)
	}
	return &execProc{cmd: cmd, out: out}, nil
}
