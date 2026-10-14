package workflowgenerate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/agentroster"
	"github.com/monoes/mono-agent/internal/ai/generate"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/workflow"
	"github.com/spf13/cobra"
)

// generateOutput is `workflow generate --json`.
type generateOutput struct {
	Saved      bool                  `json:"saved"`
	ID         string                `json:"id,omitempty"`
	Name       string                `json:"name"`
	NodeCount  int                   `json:"node_count"`
	Valid      bool                  `json:"valid"`
	Warnings   []string              `json:"warnings,omitempty"`
	Attempts   int                   `json:"attempts"`
	History    []generate.Attempt    `json:"history"`
	Runtime    string                `json:"runtime"`
	Model      string                `json:"model,omitempty"`
	Workflow   workflow.WorkflowFile `json:"workflow"`
	IsActive   bool                  `json:"is_active"`
	NextAction string                `json:"next,omitempty"`
}

func (d Deps) newCommand() *cobra.Command {
	var (
		runtime, model, name string
		maxAttempts          int
		noSave, save         bool
	)
	cmd := &cobra.Command{
		Use:   `generate "<description>"`,
		Short: "Build a workflow from a plain-language description with your AI agent (saved as an inactive draft)",
		Long: "Asks an AI agent (through monomind, on an agent CLI installed on this machine) for a workflow " +
			"matching the description, using the node catalog and the workflow JSON format. The answer is " +
			"validated like `workflow validate` (plus a check that every node type exists); problems go back " +
			"to the agent until it passes or --max-attempts turns have run. The result is saved like " +
			"`workflow save`, as a new INACTIVE workflow, unless --no-save, which prints the JSON document only. " +
			"--runtime auto (the default) uses the first ready model of the validated roster " +
			"(`monoagentcli agent roster`), else the first installed runtime. " +
			"Exit 3 when no attempt produced a valid workflow.",
		Example: `  monoagentcli workflow generate "every morning at 9 fetch https://example.com/feed.xml and email me the new items"
  monoagentcli workflow generate "summarize a URL with my agent" --runtime codex --no-save
  monoagentcli --json workflow generate "post a webhook payload to Slack" --name "Webhook to Slack"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !save {
				noSave = true
			}
			description := strings.TrimSpace(strings.Join(args, " "))
			if description == "" {
				return clicore.InvalidInput("a description is required")
			}
			if maxAttempts < 1 {
				return clicore.InvalidInput("--max-attempts must be at least 1")
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			rt, mdl, err := d.resolveRuntime(ctx, db.DB, runtime, model)
			if err != nil {
				return err
			}
			exec, err := d.ExecFor(ctx, rt, mdl)
			if err != nil {
				return err
			}
			reg := d.NodeRegistry(db.DB)
			res, err := generate.Generate(ctx, generate.Request{
				Description: description,
				Name:        name,
				MaxAttempts: maxAttempts,
				Catalog:     d.nodeCatalog(reg),
				Exec:        exec,
				Validate:    generateValidator(reg),
			})
			if err != nil {
				var ae *generate.AttemptsError
				if errors.As(err, &ae) {
					if d.JSONOutput() {
						_ = json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
							"error": err.Error(), "code": "not_valid", "valid": false,
							"attempts": len(ae.History), "history": ae.History,
						})
						return clicore.Reported{Err: clicore.InvalidInput("%v", err)}
					}
					return clicore.InvalidInput("%v", err)
				}
				return err
			}

			out := generateOutput{
				Name: res.Workflow.Name, NodeCount: len(res.Workflow.Nodes), Valid: true,
				Warnings: res.Warnings, Attempts: res.Attempts, History: res.History,
				Runtime: rt, Model: mdl, Workflow: res.Document,
			}
			if !noSave {
				store := d.WorkflowStore(db)
				saved, err := d.SaveDocument(ctx, store, d.ProfileID(), generate.Draft(res.Workflow))
				if err != nil {
					return err
				}
				generate.TagProfile(ctx, db.DB, saved, d.ProfileID())
				out.Saved, out.ID = true, saved.ID
				out.Workflow = generate.ToFile(saved)
				out.NextAction = "monoagentcli workflow get " + saved.ID
			}

			w := cmd.OutOrStdout()
			if d.JSONOutput() {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			if noSave {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				if err := enc.Encode(out.Workflow); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Valid workflow %q (%d nodes) after %d attempt(s); not saved.\n", out.Name, out.NodeCount, out.Attempts)
				return nil
			}
			fmt.Fprintf(w, "Saved draft workflow: %s  (id: %s)\n", out.Name, out.ID)
			fmt.Fprintf(w, "  nodes: %d   validation: valid   attempts: %d   runtime: %s\n", out.NodeCount, out.Attempts, runtimeLabel(rt, mdl))
			for _, warn := range out.Warnings {
				fmt.Fprintln(w, "  warning: "+warn)
			}
			fmt.Fprintln(w, "  It is inactive. Review it with `monoagentcli workflow get "+out.ID+"`, then `workflow run` or `workflow activate`.")
			return nil
		},
	}
	cmd.Flags().StringVar(&runtime, "runtime", generate.RuntimeAuto, "Agent runtime to ask (claude, codex, ...), or auto: the first ready model of the validated roster")
	cmd.Flags().StringVar(&model, "model", "", "Model of the runtime (default: the roster's pick for auto, else the runtime's default)")
	cmd.Flags().StringVar(&name, "name", "", "Name for the workflow (default: the one the agent chooses)")
	cmd.Flags().IntVar(&maxAttempts, "max-attempts", generate.DefaultMaxAttempts, "Turns to take before giving up; each failed attempt's problems go back to the agent")
	cmd.Flags().BoolVar(&noSave, "no-save", false, "Print the validated workflow JSON instead of saving it")
	cmd.Flags().BoolVar(&save, "save", true, "Save the workflow as an inactive draft (the default; --save=false is --no-save)")
	clicore.WithJSONErrors(d.JSONOutput, cmd)
	return cmd
}

func runtimeLabel(runtime, model string) string {
	if model == "" {
		return runtime
	}
	return runtime + " / " + model
}

// resolveRuntime turns --runtime auto into a roster pick.
func (d Deps) resolveRuntime(ctx context.Context, db *sql.DB, runtime, model string) (string, string, error) {
	runtime = strings.TrimSpace(runtime)
	if runtime != "" && runtime != generate.RuntimeAuto {
		return runtime, model, nil
	}
	results, _ := agentroster.List(ctx, db)
	scan, err := d.Scan(ctx)
	if err != nil {
		scan = nil
	}
	rt, mdl, err := generate.PickRuntime(results, scan)
	if err != nil {
		return "", "", monomind.MarkNotSetup(err)
	}
	if model != "" {
		mdl = model
	}
	return rt, mdl, nil
}

// nodeCatalog is the registry's node types plus the triggers, titled
// from the `ref node` docs.
func (d Deps) nodeCatalog(reg *workflow.NodeTypeRegistry) []generate.NodeInfo {
	types := append(reg.Types(), generate.TriggerTypes()...)
	return generate.BuildCatalog(types,
		func(t string) string {
			if d := d.FindNodeDoc(t); d != nil {
				return d.Short
			}
			return ""
		},
		func(t string) string {
			if d := d.FindNodeDoc(t); d != nil {
				return d.Outputs
			}
			return ""
		})
}

// generateValidator is the check a generated workflow must pass: what
// `workflow validate` runs, with unknown node types as errors.
func generateValidator(reg *workflow.NodeTypeRegistry) generate.ValidateFunc {
	return generate.Validator(reg.Has)
}
