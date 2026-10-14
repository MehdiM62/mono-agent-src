package workflowio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/monoes/mono-agent/internal/n8nimport"
	"github.com/spf13/cobra"
)

// n8nIncompleteError is returned (after the result was printed) when an
// n8n workflow converted with placeholder nodes or validation errors: exit
// code 3.
func (d Deps) n8nIncompleteError(rep *n8nimport.Report) error {
	if rep.Counts.Unmapped == 0 {
		return d.Reported(d.InvalidInput("n8n import incomplete: the converted workflow has %d validation error(s) (see the report)", rep.Counts.ValidationErrors))
	}
	return d.Reported(d.InvalidInput("n8n import incomplete: %d node(s) have no mono-agent equivalent and fail until replaced (see the report)", rep.Counts.Unmapped))
}

// readImportSource reads the workflow file named by the positional
// argument or --file, or stdin for "-" / nothing.
func (d Deps) readImportSource(cmd *cobra.Command, args []string) ([]byte, error) {
	path, _ := cmd.Flags().GetString("file")
	if len(args) > 0 {
		if path != "" && path != args[0] {
			return nil, d.InvalidInput("give the file either as an argument or with --file, not both")
		}
		path = args[0]
	}
	var raw []byte
	var err error
	if path == "" || path == "-" {
		raw, err = io.ReadAll(cmd.InOrStdin())
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, d.NotFound("read input: %v", err)
		}
		return nil, fmt.Errorf("read input: %w", err)
	}
	return raw, nil
}

// convertN8n converts n8n workflow JSON, mapping bad input to exit code 3.
func (d Deps) convertN8n(raw []byte) ([]byte, *n8nimport.Report, error) {
	file, rep, err := n8nimport.Convert(raw)
	if err != nil {
		return nil, nil, d.InvalidInput("%v", err)
	}
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("encode converted workflow: %w", err)
	}
	return out, rep, nil
}

// withN8nImport adds --from n8n to `workflow import`: the n8n export is
// converted, then imported by the unchanged import command (fed the mono
// workflow on stdin), and the conversion report is printed with its
// result. The file may also be given as a positional argument.
func (d Deps) withN8nImport(cmd *cobra.Command) {
	var from string
	cmd.Flags().StringVar(&from, "from", "", `Convert from another tool's format first: "n8n" (an n8n workflow export; prints a conversion report, exit 3 when a node has no equivalent or the result fails validate)`)
	cmd.Use = "import [file|-]"
	cmd.Args = cobra.MaximumNArgs(1)
	run := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		switch strings.ToLower(from) {
		case "":
			if len(args) == 1 && args[0] != "-" {
				if err := c.Flags().Set("file", args[0]); err != nil {
					return err
				}
			}
			return run(c, nil)
		case "n8n":
		default:
			return d.InvalidInput("--from %q: only \"n8n\" is supported", from)
		}
		raw, err := d.readImportSource(c, args)
		if err != nil {
			return err
		}
		converted, rep, err := d.convertN8n(raw)
		if err != nil {
			return err
		}
		if err := c.Flags().Set("file", ""); err != nil {
			return err
		}
		out := c.OutOrStdout()
		var buf bytes.Buffer
		c.SetIn(bytes.NewReader(converted))
		c.SetOut(&buf)
		err = run(c, nil)
		c.SetOut(out)
		if err != nil {
			_, _ = out.Write(buf.Bytes())
			return err
		}
		if d.JSONOutput() {
			var result map[string]interface{}
			if jerr := json.Unmarshal(buf.Bytes(), &result); jerr != nil {
				return fmt.Errorf("read import result: %w", jerr)
			}
			result["n8n"] = rep
			if err := json.NewEncoder(out).Encode(result); err != nil {
				return err
			}
		} else {
			_, _ = out.Write(buf.Bytes())
			fmt.Fprintln(out)
			fmt.Fprint(out, rep.Text())
		}
		if !rep.Complete {
			return d.n8nIncompleteError(rep)
		}
		return nil
	}
}

// newWorkflowConvertCmd converts a workflow from another tool's format and
// prints it without saving anything.
func (d Deps) newConvertCmd() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "convert [file|-]",
		Short: "Convert an n8n workflow export to a mono-agent workflow and print it with a report (nothing is saved)",
		Long: `Convert an n8n workflow export (JSON from n8n's "Download" or "n8n export:workflow")
into a mono-agent workflow. Nothing is saved: the workflow JSON goes to stdout and
the conversion report to stderr, so the output can be piped into
"workflow import". With --json, stdout holds {"workflow": …, "report": …}.

Exit code 3 when a node has no mono-agent equivalent (it becomes a placeholder
that fails until replaced), when the result fails "workflow validate" (the report
lists the errors; a field a listed connection supplies does not count), or the
input is not an n8n workflow.`,
		Example: `  monoagentcli workflow convert --from n8n my-flow.json > mono-flow.json
  monoagentcli --json workflow convert --from n8n my-flow.json | jq .report.counts
  monoagentcli workflow import --from n8n my-flow.json   # convert and save in one step`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if strings.ToLower(from) != "n8n" {
				return d.InvalidInput("--from %q: only \"n8n\" is supported", from)
			}
			raw, err := d.readImportSource(c, args)
			if err != nil {
				return err
			}
			converted, rep, err := d.convertN8n(raw)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if d.JSONOutput() {
				err = json.NewEncoder(out).Encode(map[string]interface{}{"workflow": json.RawMessage(converted), "report": rep})
			} else {
				_, err = fmt.Fprintln(out, string(converted))
				fmt.Fprint(c.ErrOrStderr(), rep.Text())
			}
			if err != nil {
				return err
			}
			if !rep.Complete {
				return d.n8nIncompleteError(rep)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "n8n", `Source format (only "n8n")`)
	cmd.Flags().StringP("file", "f", "", "Path to the export (default: the argument, or stdin)")
	d.WithJSONErrors(cmd)
	return cmd
}
