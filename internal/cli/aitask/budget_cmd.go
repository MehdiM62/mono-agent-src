package aitask

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/aitask/budget"
	chatcli "github.com/monoes/mono-agent/internal/cli/chat"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// ledger is the budget ledger over the service's database. Its operator
// ceiling is the coder budget (`coder set --budget-usd`).
func ledger(svc *aitask.Service) *budget.Ledger {
	l := &budget.Ledger{DB: svc.Store.DB}
	if s, err := chatcli.LoadSettings(svc.Store.DB); err == nil {
		l.Operator = s.BudgetUSD
	}
	return l
}

// budgetCmd is `aitask budget`: show the caps and what they have used, set
// them (the operator's), lower them (the operator's, or a task's own session
// for its own conversation).
func (c *cli) budgetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Spend caps for AI tasks, per kind, conversation, role or all",
		Long: "A task runs under the lowest cap that applies to it (global, its kind, its conversation, its role), and never above " +
			"the coder budget (`coder set --budget-usd`). A cap of 0 means no spend, never unlimited, and no cap at all means " +
			"the task does not run. `set` raises or changes a cap and is the user's alone; `lower` can only lower the cap of " +
			"a task's own conversation, and is allowed to that task's session.",
	}
	for _, sub := range []*cobra.Command{c.budgetShow(), c.budgetSet(), c.budgetLower()} {
		clicore.WithJSONErrors(c.d.JSONOutput, sub)
		cmd.AddCommand(sub)
	}
	return cmd
}

func (c *cli) budgetShow() *cobra.Command {
	var taskID string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the caps and what they have used (the user's), or one task's cap and spend (--task)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, closeDB, err := c.open()
			if err != nil {
				return err
			}
			defer closeDB()
			if err := c.authorize(svc.Store, "show AI task budgets", taskID, taskID != ""); err != nil {
				return err
			}
			l := ledger(svc)
			if taskID != "" {
				if _, err := svc.Store.Get(taskID); err != nil {
					return cliErr(err)
				}
				tb, err := l.TaskStatus(cmd.Context(), taskID)
				if err != nil {
					return cliErr(err)
				}
				return c.print(cmd, tb, fmt.Sprintf("%s  spent $%.4f of $%.4f", tb.TaskID, tb.SpentUSD, tb.CapUSD))
			}
			rep, err := l.Report(cmd.Context())
			if err != nil {
				return cliErr(err)
			}
			lines := []string{fmt.Sprintf("coder budget: $%g", rep.OperatorUSD)}
			for _, r := range rep.Caps {
				lines = append(lines, fmt.Sprintf("%s %s: cap $%g, used $%.4f", r.Scope, r.Key, r.CapUSD, r.UsedUSD))
			}
			return c.print(cmd, rep, strings.Join(lines, "\n"))
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "Show this task's cap and spend (the task's own session may)")
	return cmd
}

func (c *cli) budgetSet() *cobra.Command {
	var scope, key string
	var usd float64
	var clear bool
	cmd := &cobra.Command{
		Use:   "set --scope global|kind|conversation|role [--key K] --usd N",
		Short: "Set or change a cap, up or down (the user's alone); --clear removes it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := clicore.RequireOperator(c.d.RequireOperator, "set an AI task budget"); err != nil {
				return err
			}
			if !clear && !cmd.Flags().Changed("usd") {
				return clicore.InvalidInput("--usd is required (0 means no spend)")
			}
			svc, closeDB, err := c.open()
			if err != nil {
				return err
			}
			defer closeDB()
			l := ledger(svc)
			if clear {
				err = l.ClearCap(scope, key)
			} else {
				err = l.SetCap(scope, key, usd, "operator")
			}
			if err != nil {
				return cliErr(err)
			}
			return c.print(cmd, map[string]any{"scope": scope, "key": key, "capUsd": usd, "cleared": clear}, "ok")
		},
	}
	f := cmd.Flags()
	f.StringVar(&scope, "scope", "", "global, kind, conversation or role")
	f.StringVar(&key, "key", "", "The kind, conversation id or role (none for global)")
	f.Float64Var(&usd, "usd", 0, "The cap in USD; 0 means no spend")
	f.BoolVar(&clear, "clear", false, "Remove the cap instead")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

func (c *cli) budgetLower() *cobra.Command {
	var taskID, conversationID string
	var usd float64
	cmd := &cobra.Command{
		Use:   "lower (--task ID | --conversation ID) --usd N",
		Short: "Lower the cap of a chat (never raises it)",
		Long: "--task lowers the spend cap of the task's conversation to N, or refuses when N is above the cap that applies now " +
			"(cap_raise_refused). --conversation creates or lowers the cap of that conversation, only to a value strictly below the " +
			"cap that applies to it now (cap_raise_refused otherwise, no_cap_to_lower when no cap applies at all). The user may lower " +
			"any; a task's own session only its own task's conversation.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (taskID == "") == (conversationID == "") || !cmd.Flags().Changed("usd") {
				return clicore.InvalidInput("--usd and exactly one of --task and --conversation are required")
			}
			svc, closeDB, err := c.open()
			if err != nil {
				return err
			}
			defer closeDB()
			if conversationID != "" {
				by := "operator"
				if id := os.Getenv(aitask.EnvTaskID); id != "" && holder(svc.Store, id) {
					if t, err := svc.Store.Get(id); err == nil && t.ConversationID == conversationID {
						by = "task:" + id
					}
				}
				if by == "operator" {
					if err := clicore.RequireOperator(c.d.RequireOperator, "lower an AI task budget"); err != nil {
						return err
					}
				}
				if err := ledger(svc).LowerConversationCap(conversationID, usd, by); err != nil {
					return cliErr(err)
				}
				return c.print(cmd, map[string]any{"conversationId": conversationID, "capUsd": usd}, "ok")
			}
			by := "operator"
			if holder(svc.Store, taskID) {
				by = "task:" + taskID
			} else if err := clicore.RequireOperator(c.d.RequireOperator, "lower an AI task budget"); err != nil {
				return err
			}
			t, err := svc.Store.Get(taskID)
			if err != nil {
				return cliErr(err)
			}
			if err := ledger(svc).LowerCap(budget.ScopeConversation, t.ConversationID, usd, t.Kind, by); err != nil {
				return cliErr(err)
			}
			return c.print(cmd, map[string]any{"taskId": taskID, "conversationId": t.ConversationID, "capUsd": usd}, "ok")
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "The task whose conversation's cap is lowered")
	cmd.Flags().StringVar(&conversationID, "conversation", "", "The conversation whose cap is created or lowered")
	cmd.Flags().Float64Var(&usd, "usd", 0, "The new cap in USD (0 means no spend)")
	return cmd
}
