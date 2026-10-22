package applypark

import (
	"context"
	"fmt"
)

// Observation is what the extension saw in a tab: a trusted submit followed by
// a navigation or an in-page confirmation change.
type Observation struct {
	TabID  int
	Origin string
	URL    string
}

// Observe matches the observation to the one parked application on that tab
// and origin and records submit_observed_at on its fill. It reports whether an
// application matched. It only records a hint: the application's status stays
// awaiting_submit (or whatever it was) and is never set to applied here; the
// person confirms with `application send`. An ambiguous match (the same tab id
// in two profiles) matches nothing.
func Observe(ctx context.Context, db DBTX, o Observation) (appID string, matched bool, err error) {
	if o.TabID <= 0 || o.TabID > maxTabID {
		return "", false, invalid("tab id %d is out of range", o.TabID)
	}
	origin, err := ParseOrigin(o.Origin)
	if err != nil {
		return "", false, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT t.application_id FROM application_tabs t JOIN applications a ON a.id = t.application_id
		 WHERE t.tab_id = ? AND t.origin = ? AND t.state = 'parked' AND a.status IN ('awaiting_submit', 'needs_you')`,
		o.TabID, origin)
	if err != nil {
		return "", false, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", false, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != 1 {
		return "", false, rows.Err()
	}
	res, err := db.ExecContext(ctx,
		`UPDATE application_fills SET submit_observed_at = ? WHERE application_id = ?`, now(), ids[0])
	if err != nil {
		return "", false, fmt.Errorf("recording the observed submit: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", false, nil
	}
	return ids[0], true, nil
}

// ClearObserved forgets an observed submit, for a new fill of the application.
func ClearObserved(ctx context.Context, db DBTX, appID string) error {
	_, err := db.ExecContext(ctx, `UPDATE application_fills SET submit_observed_at = '' WHERE application_id = ?`, appID)
	return err
}

// ObservedAt is when a submit was observed on the application's tab; "" when none.
func ObservedAt(ctx context.Context, db DBTX, appID string) (string, error) {
	var at string
	err := db.QueryRowContext(ctx, `SELECT submit_observed_at FROM application_fills WHERE application_id = ?`, appID).Scan(&at)
	if err != nil {
		return "", nil
	}
	return at, nil
}
