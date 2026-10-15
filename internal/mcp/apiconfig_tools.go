package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/monoes/mono-agent/internal/apiconfig"
	"github.com/monoes/mono-agent/internal/autostart"
	"github.com/monoes/mono-agent/internal/openaiapi"
)

// apiConfigTools are the tools over the OpenAI-compatible API's server: where it listens, the
// settings saved for it, and what makes them take effect. Their documents are internal/apiconfig's,
// the same code `monoagentcli api status` and `api config` run, so that each tool answers exactly
// what its command answers (`--json`), and the pipe tests in cmd/monoagentcli compare the two.
func apiConfigTools() []tool {
	tools := []tool{
		{
			name:        "api_status",
			description: "Show where the OpenAI-compatible API (/v1) listens and whether it answers: the document of `monoagentcli api status --json`. It probes each listener over HTTP (a few seconds when nothing answers), changes nothing and returns no key. " + apiToolsDetails,
			schema:      objSchema(nil),
			annotations: map[string]bool{"readOnlyHint": true, "idempotentHint": true},
			handler:     toolAPIStatus,
		},
		{
			name:        "api_config_get",
			global:      true, // reads no profile data
			description: "Show the ten saved settings of the OpenAI-compatible API's server, what a server started now would use and what the running daemon started with, each with its state (applied, pending_restart, overridden, ...): the document of `monoagentcli api config show --json`. It changes nothing. " + apiToolsDetails,
			schema:      objSchema(nil),
			annotations: map[string]bool{"readOnlyHint": true, "idempotentHint": true},
			handler:     toolAPIConfigGet,
		},
		apiConfigSetTool(),
		{
			name:        "api_config_apply",
			global:      true, // reads no profile data
			description: "Restart the daemon through the service it is registered with so that it reads the saved settings: the document of `monoagentcli daemon restart --json`. It interrupts whatever the daemon is running (workflows, org runs); use it when api_config_get says restart_needed. " + apiToolsDetails,
			schema:      objSchema(nil),
			annotations: map[string]bool{"readOnlyHint": false, "destructiveHint": true},
			mutating:    true,
			handler:     toolAPIConfigApply,
		},
	}
	return append(tools, apiAutoTools()...)
}

// damagedRowNote is what the description of every tool that reads the saved settings tells a model of a
// row that cannot be read: the message starts the same way for every kind of damage, the user has a
// command for it, and the operator can allow api_config_set to do it. A row that a newer version saved
// is not damaged, and nothing removes it.
const damagedRowNote = "If the saved settings are damaged (a row that cannot be read) it fails with a message that starts `the saved settings are damaged`: " +
	"tell the user, who can run `monoagentcli api config unset --all --yes`, or the operator can allow api_config_set to remove the row (unset all, with --allow-api-exposure); " +
	"a row that a newer version saved fails too, and nothing here removes it."

// apiEnv is what the documents of the tools read of this process: Options.APIEnv, whose zero value
// is the process's own. Whose environment they describe is always this MCP server's.
func (s *Server) apiEnv() apiconfig.Env {
	env := s.opts.APIEnv
	env.Environment = openaiapi.ReportSourceMCP
	return env
}

func toolAPIStatus(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	st, err := apiconfig.BuildStatus(ctx, rt.db.DB, s.apiEnv(), profileOf(ctx))
	if err != nil {
		var input *apiconfig.InputError
		if errors.As(err, &input) {
			// The shared parsers quote the value they refuse, which suits a command line: what this
			// server's environment holds is not repeated to a model. api_models_list names which
			// variable it is.
			return nil, errBadEnvironment
		}
		return nil, err
	}
	return st, nil
}

func toolAPIConfigGet(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	return apiconfig.Show(ctx, rt.db.DB, s.apiEnv())
}

// getenv reads this server's environment: Options.APIEnv's, else the process's own.
func (s *Server) getenv() func(string) string {
	if g := s.opts.APIEnv.Getenv; g != nil {
		return g
	}
	return os.Getenv
}

// installer is the service manager: Options.APIEnv's, else this system's own.
func (s *Server) installer() autostart.Installer {
	if in := s.opts.APIEnv.Installer; in != nil {
		return in
	}
	return autostart.New()
}

// toolAPIConfigApply restarts the daemon as `daemon restart` does, and answers with the words of
// that command: its document, and its errors (settings that cannot be used, a daemon that is not
// registered, a service manager that fails). It takes no argument: what it applies is what is saved,
// and it reads that first: a daemon that cannot use the saved settings starts without the API, so a
// restart would replace one that serves it with one that does not. The error is the one of every tool
// that reads them, and nothing is restarted.
func toolAPIConfigApply(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
	rt, err := s.runtime()
	if err != nil {
		return nil, err
	}
	if _, err := apiconfig.LoadValid(ctx, rt.db.DB); err != nil {
		return nil, err
	}
	res, err := autostart.RestartRegistered(ctx, s.installer())
	if err != nil {
		var notRegistered *autostart.NotRegisteredError
		if errors.As(err, &notRegistered) {
			return nil, err
		}
		return nil, fmt.Errorf("restart: %w", err)
	}
	return res, nil
}

// errBadEnvironment is the answer to a value in this server's environment that fails its rule.
var errBadEnvironment = errors.New("MONOAGENT_API_CONFINEMENT, MONOAGENT_API_CONTEXT_CONFINEMENT and MONOAGENT_API_AUTO_CONFINEMENT in this MCP server's environment must each be chat-only, sandboxed or any: one of them is not (api_models_list says which)")
