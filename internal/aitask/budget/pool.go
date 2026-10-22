package budget

import (
	"context"

	"github.com/monoes/mono-agent/internal/aitask"
)

// ReservePool reserves up to want USD from a pool of poolUSD (a turn's share
// for its workers), for one worker run, and returns what it was given. The
// pool's active reservations plus what settled runs spent never exceed
// poolUSD, however many workers ask at once: the grant is one conditional
// INSERT. Nothing left is no_budget.
func (l *Ledger) ReservePool(ctx context.Context, poolKey string, poolUSD, want float64) (resID string, granted float64, err error) {
	if poolUSD <= epsilon {
		return "", 0, noBudget("no budget is left for workers")
	}
	const used = `(SELECT COALESCE(SUM(CASE WHEN settled = 1 THEN spent_usd ELSE reserved_usd END), 0) FROM ai_budget_pool WHERE pool_key = ?)`
	for attempt := 0; attempt < 5; attempt++ {
		var u float64
		if err := l.DB.QueryRowContext(ctx, `SELECT `+used, poolKey).Scan(&u); err != nil {
			return "", 0, err
		}
		granted = poolUSD - u
		if want > 0 && want < granted {
			granted = want
		}
		if granted <= epsilon {
			return "", 0, noBudget("the workers' share of this task's budget ($%.4f) is spent or reserved", poolUSD)
		}
		resID = aitask.NewID()
		res, err := l.DB.ExecContext(ctx, `INSERT INTO ai_budget_pool (res_id, pool_key, reserved_usd, created_at)
			SELECT ?, ?, ?, ? WHERE `+used+` + ? <= ?`, resID, poolKey, granted, l.now(), poolKey, granted, poolUSD+epsilon)
		if err != nil {
			return "", 0, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return resID, granted, nil
		}
	}
	return "", 0, noBudget("the workers' share of this task's budget is being taken by other workers")
}

// SettlePool ends a worker run's reservation: from now it counts spent (its
// reservation when the run reported no cost, pass that).
func (l *Ledger) SettlePool(ctx context.Context, resID string, spent float64) error {
	_, err := l.DB.ExecContext(ctx, `UPDATE ai_budget_pool SET settled = 1, spent_usd = ? WHERE res_id = ?`, spent, resID)
	return err
}
