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

// cliCoder is the real coderCLI: this binary, with every agent-context
// variable removed from the child's env (coderAgentEnv). The request came
// from the person's browser, so a bridge that happens to run under an
// agent's shell must not make it an agent's request.
type cliCoder struct{ chat cliChatBackend }

// coderAgentEnv is every variable the `browser` commands' agent detector
// reads: browserctl.AgentContextEnv, which this package can't import
// (browserctl imports it). A test holds the two lists equal.
func coderAgentEnv() []string {
	return append(orgsign.AgentContextMarkers(), hilsend.EnvAgentContext, "MONOAGENT_ACTOR")
}

func (c *cliCoder) command(ctx context.Context, stdin string, args []string) (*exec.Cmd, error) {
	bin, err := c.chat.binary()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = monomindWaitDelay
	cmd.Stdin = strings.NewReader(stdin)
	markers := coderAgentEnv()
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); !slices.Contains(markers, k) {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	return cmd, nil
}

func (c *cliCoder) Run(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd, err := c.command(ctx, stdin, args)
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
	cmd, err := c.command(ctx, stdin, args)
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
