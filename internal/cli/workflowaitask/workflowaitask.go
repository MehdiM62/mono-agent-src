// Package workflowaitask is `workflow apply-fix` and `workflow save-draft`: the
// user's side of the AI task kinds failed-run-fix and workflow-generate. The
// agent of those kinds only proposes (its result is data); these commands turn
// a finished proposal into a change, only for the operator, after the user has
// looked at it (apply-fix --dry-run shows the exact edits). Neither approves
// what it writes: the workflow stays unapproved until the user saves or
// activates it themselves.
package workflowaitask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/aitaskkinds"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/storage"
	"github.com/monoes/mono-agent/internal/workflow"
	"github.com/monoes/mono-agent/internal/workflowfix"
)

// Deps is what the commands take from the CLI around them.
type Deps struct {
	JSONOutput func() bool
	ProfileID  func() string
	OpenDB     func() (*storage.Database, error)
	// RequireOperator refuses an agent, the chat's runs and workflow nodes.
	RequireOperator func(what string) error
	// WorkflowStore is the profile's workflows, files and SQLite alike.
	WorkflowStore func(db *storage.Database) *workflow.HybridWorkflowStore
	// Wire runs before a command reads workflows (it sets up workflowfix).
	Wire func()
}

// Typed refusals, in --json as "code".
const (
	CodeNotFound    = "not_found"
	CodeWrongKind   = "wrong_kind"
	CodeNotComplete = "not_complete"
	CodeNoResult    = "no_result"
	CodeBadResult   = "bad_result"
)

type coded struct {
	exit   int
	msg    string
	fields map[string]any
}

func (e *coded) Error() string                   { return e.msg }
func (e *coded) JSONErrorFields() map[string]any { return e.fields }
func (e *coded) Unwrap() error                   { return &clicore.Error{Code: e.exit, Msg: e.msg} }

func refuse(code, format string, a ...any) error {
	exit := 3
	if code == CodeNotFound {
		exit = 2
	}
	return &coded{exit: exit, msg: fmt.Sprintf(format, a...), fields: map[string]any{"code": code}}
}

// cliErr gives a workflowfix refusal its exit code and --json code.
func cliErr(err error) error {
	var fe *workflowfix.Error
	if errors.As(err, &fe) {
		return refuse(fe.Code, "%s", fe.Message)
	}
	return err
}

func (d Deps) print(cmd *cobra.Command, v any, human string) error {
	if d.JSONOutput() {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(v)
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), human)
	return err
}

// task returns this profile's finished task id of the given kind, with its
// stored result. The result is read from the task row only, never from a path.
func (d Deps) task(db *storage.Database, id, kind string) (aitask.Task, error) {
	t, err := (&aitask.Store{DB: db.DB, Profile: d.ProfileID()}).Get(id)
	if err != nil {
		var ae *aitask.Error
		if errors.As(err, &ae) && ae.Code == aitask.CodeNotFound {
			return t, refuse(CodeNotFound, "no AI task %s in this profile", id)
		}
		return t, err
	}
	switch {
	case t.Kind != kind:
		return t, refuse(CodeWrongKind, "task %s is a %s task, not %s", id, t.Kind, kind)
	case t.Status != aitask.StatusComplete:
		return t, refuse(CodeNotComplete, "task %s is %s: only a complete task has a result to use", id, t.Status)
	case len(t.Result) == 0:
		return t, refuse(CodeNoResult, "task %s has no result", id)
	}
	return t, nil
}

// New returns the two commands, for `workflow`.
func New(d Deps) []*cobra.Command {
	cmds := []*cobra.Command{d.applyFix(), d.saveDraft()}
	for _, c := range cmds {
		clicore.WithJSONErrors(d.JSONOutput, c)
	}
	return cmds
}

func (d Deps) applyFix() *cobra.Command {
	var dryRun, allowActive bool
	cmd := &cobra.Command{
		Use:   "apply-fix <task-id>",
		Short: "Show or apply the workflow edits a finished failed-run-fix task proposed (the user's own)",
		Long: "Reads the result of a finished failed-run-fix task of this profile and plans its edits against the workflow as it is now. " +
			"--dry-run prints them (node, path, before, after) and warnings (workflow_active, new_outbound, credential_field, expression_added) and changes nothing. " +
			"Without it the edits are stored; the workflow is then UNAPPROVED (a person saves or activates it to approve it again). " +
			"An active workflow is refused unless --allow-active. The agent only proposed: this command is the operator's. " + clicore.OperatorOnlyHelp,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := clicore.RequireOperator(d.RequireOperator, "apply an AI task's workflow fix"); err != nil {
				return err
			}
			if d.Wire != nil {
				d.Wire()
			}
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			t, err := d.task(db, args[0], aitaskkinds.FailedRunFix)
			db.Close()
			if err != nil {
				return err
			}
			ctx := context.Background()
			plan, err := workflowfix.Plan(ctx, d.ProfileID(), t.Result)
			if err != nil {
				return cliErr(err)
			}
			if dryRun {
				return d.print(cmd, map[string]any{"ok": true, "workflowId": plan.WorkflowID, "workflowName": plan.WorkflowName,
					"edits": plan.Edits, "warnings": plan.Warnings},
					fmt.Sprintf("%d edit(s) to %s (%s); %d warning(s)", len(plan.Edits), plan.WorkflowName, plan.WorkflowID, len(plan.Warnings)))
			}
			n, err := workflowfix.Apply(ctx, d.ProfileID(), t.Result, allowActive)
			if err != nil {
				return cliErr(err)
			}
			return d.print(cmd, map[string]any{"ok": true, "workflowId": plan.WorkflowID, "applied": n, "needsApproval": true},
				fmt.Sprintf("applied %d edit(s) to %s; save or activate it to approve it again", n, plan.WorkflowName))
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the edits and warnings; change nothing")
	cmd.Flags().BoolVar(&allowActive, "allow-active", false, "Apply to a workflow that is active")
	return cmd
}

func (d Deps) saveDraft() *cobra.Command {
	return &cobra.Command{
		Use:   "save-draft <task-id>",
		Short: "Save the workflow a finished workflow-generate task proposed, as an inactive, unapproved draft (the user's own)",
		Long: "Reads the result of a finished workflow-generate task of this profile, checks the workflow again with the validator `workflow validate` uses, " +
			"and saves it as a NEW inactive workflow (it never overwrites one). The draft is unapproved: a person saves or activates it to approve it. " + clicore.OperatorOnlyHelp,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := clicore.RequireOperator(d.RequireOperator, "save an AI task's generated workflow"); err != nil {
				return err
			}
			if d.Wire != nil {
				d.Wire()
			}
			db, err := d.OpenDB()
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()
			t, err := d.task(db, args[0], aitaskkinds.WorkflowGenerate)
			if err != nil {
				return err
			}
			var res struct {
				Workflow json.RawMessage `json:"workflow"`
			}
			if err := json.Unmarshal(t.Result, &res); err != nil || len(res.Workflow) == 0 {
				return refuse(CodeBadResult, "task %s has no workflow in its result", t.ID)
			}
			if err := aitaskkinds.CheckResult(context.Background(), d.ProfileID(), aitaskkinds.WorkflowGenerate, t.Result); err != nil {
				return refuse(CodeBadResult, "%v", err)
			}
			doc, err := workflow.ParseWorkflowFileBytes(res.Workflow)
			if err != nil {
				return refuse(CodeBadResult, "the workflow does not parse: %v", err)
			}
			wf, err := d.save(context.Background(), db, doc)
			if err != nil {
				return err
			}
			return d.print(cmd, map[string]any{"ok": true, "workflowId": wf.ID, "name": wf.Name, "nodeCount": len(wf.Nodes), "needsApproval": true},
				fmt.Sprintf("saved draft %s (%s), inactive; approve it by saving or activating it", wf.Name, wf.ID))
		},
	}
}

// save stores doc as a new inactive workflow of the profile under a fresh id,
// whatever id or active flag the document carries, and records no approval.
func (d Deps) save(ctx context.Context, db *storage.Database, doc workflow.Workflow) (*workflow.Workflow, error) {
	wf := &workflow.Workflow{
		ID:          uuid.New().String(),
		Name:        doc.Name,
		Description: doc.Description,
		IsActive:    false,
		Version:     1,
		ProfileID:   d.ProfileID(),
		Settings:    doc.Settings,
	}
	for _, n := range doc.Nodes {
		if n.Schema == nil {
			n.Schema, _ = workflow.LoadDefaultSchema(n.Type)
		}
		n.WorkflowID = ""
		wf.Nodes = append(wf.Nodes, n)
	}
	for _, c := range doc.Connections {
		c.WorkflowID = ""
		wf.Connections = append(wf.Connections, c)
	}
	if err := d.WorkflowStore(db).SaveWorkflow(ctx, wf); err != nil {
		return nil, fmt.Errorf("save workflow: %w", err)
	}
	return wf, nil
}
