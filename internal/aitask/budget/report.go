package budget

import (
	"context"
	"database/sql"

	"github.com/monoes/mono-agent/internal/aitask"
)

// CapUsage is a stored cap with what the ledger counts against it.
type CapUsage struct {
	Row
	UsedUSD float64 `json:"usedUsd"`
}

// Report is `aitask budget show`: the operator's coder budget, every cap and
// what each has used.
type Report struct {
	OperatorUSD float64    `json:"operatorUsd"`
	Caps        []CapUsage `json:"caps"`
	SpentUSD    float64    `json:"spentUsd"`
}

// Report lists the caps with their use. It first brings the ledger up to date.
func (l *Ledger) Report(ctx context.Context) (Report, error) {
	l.syncOpen(ctx)
	caps, err := l.Caps()
	if err != nil {
		return Report{}, err
	}
	out := Report{OperatorUSD: l.Operator, Caps: []CapUsage{}}
	for _, c := range caps {
		u := CapUsage{Row: c}
		var q string
		var args []any
		switch c.Scope {
		case ScopeGlobal:
			q = `SELECT COALESCE(SUM(` + taskUsage + `), 0) FROM ai_budget_reservations r`
		case ScopeKind:
			q, args = `SELECT COALESCE(SUM(`+taskUsage+`), 0) FROM ai_budget_reservations r WHERE r.kind = ?`, []any{c.Key}
		case ScopeConversation:
			q, args = `SELECT COALESCE(SUM(`+taskUsage+`), 0) FROM ai_budget_reservations r WHERE r.conversation_id = ?`, []any{c.Key}
		case ScopeRole:
			q, args = `SELECT COALESCE(SUM(spent_usd), 0) FROM ai_budget_spend WHERE role = ?`, []any{c.Key}
		}
		if err := l.DB.QueryRowContext(ctx, q, args...).Scan(&u.UsedUSD); err != nil {
			return Report{}, err
		}
		out.Caps = append(out.Caps, u)
	}
	err = l.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(spent_usd), 0) FROM ai_budget_spend`).Scan(&out.SpentUSD)
	return out, err
}

// TaskBudget is one task's cap and spend, for the task's header in the app.
type TaskBudget struct {
	TaskID   string  `json:"taskId"`
	CapUSD   float64 `json:"capUsd"`
	SpentUSD float64 `json:"spentUsd"`
	// Reported is false while no cost has been reported for the task: the
	// spend shown is then unknown, not zero.
	Reported bool `json:"reported"`
}

// TaskStatus is task id's cap (the one its turn runs under) and what its
// conversation has spent so far.
func (l *Ledger) TaskStatus(ctx context.Context, taskID string) (TaskBudget, error) {
	var conv string
	out := TaskBudget{TaskID: taskID}
	err := l.DB.QueryRowContext(ctx, `SELECT conversation_id, budget_usd FROM ai_tasks WHERE id = ?`, taskID).Scan(&conv, &out.CapUSD)
	if err == sql.ErrNoRows {
		return out, &aitask.Error{Code: aitask.CodeNotFound, Message: "no AI task " + taskID}
	}
	if err != nil {
		return out, err
	}
	if err := l.Sync(ctx, taskID, conv); err != nil {
		return out, err
	}
	err = l.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(spent_usd), 0) FROM ai_budget_spend WHERE task_id = ?`, taskID).Scan(&out.SpentUSD)
	out.Reported = out.SpentUSD > 0
	return out, err
}
