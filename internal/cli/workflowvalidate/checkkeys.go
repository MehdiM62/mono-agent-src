package workflowvalidate

import (
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/checkkeys"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// NewCheckKeysCommand is `workflow check-keys`: the keys the app nodes of
// workflow or template files send that their ops don't declare, judged on
// the built-in catalog (internal/checkkeys). An agent's run, or an
// unapproved workflow's, of such a node waits for approval.
func NewCheckKeysCommand(jsonOutput func() bool) *cobra.Command {
	return &cobra.Command{
		Use:   "check-keys [file.json…|-]",
		Short: "List the keys app nodes send that their operations don't declare",
		Long: "Reads workflow or template JSON files (stdin with - or no file) and, for every app node in them " +
			"(a workflow nested in a template or an org included), lists the keys its config sends that its " +
			"operation doesn't declare, judged on the built-in API catalog: a key of a collection the request " +
			"merges whole, a key of an option object, or either given as an expression. An agent's run, or an " +
			"unapproved workflow's, of such a node waits for approval. It opens no database and reads no installed " +
			"package. Signed out, run `go run ./scripts/checkkeys` from a source checkout instead: the same check. " +
			"--json prints {clean, files, app_nodes, findings[{file, node_id, node, type, op, key}], errors}. " +
			"Exit 0 when clean, 1 when a node has an undeclared key, 3 when a file can't be read or isn't JSON " +
			"(the other files are still checked).",
		Example: `  monoagentcli workflow check-keys library/*.json
  monoagentcli --json workflow check-keys template.json
  cat template.json | monoagentcli workflow check-keys -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := checkkeys.Check(cmd.InOrStdin(), args)
			if err != nil {
				return err
			}
			asJSON := jsonOutput()
			rep.Write(cmd.OutOrStdout(), asJSON)
			code := rep.ExitCode()
			if code == checkkeys.ExitClean {
				return nil
			}
			err = &clicore.Error{Code: code, Msg: rep.Reason()}
			switch {
			case asJSON:
				return clicore.Reported{Err: err} // the report is the document
			case code == checkkeys.ExitUndeclared:
				return clicore.Shown{Err: err} // the report showed the keys
			}
			return err // the files it couldn't check, on stderr
		},
	}
}
