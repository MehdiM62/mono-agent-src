package applypark

import "context"

// RecordOpen notes that task opened tab. The browser commands call it when an
// apply task runs `browser open`; Bind accepts a claim only for such a tab, so
// one task cannot claim a tab another task (or the person) opened.
func (s Store) RecordOpen(ctx context.Context, taskID string, tabID int) error {
	if taskID == "" || tabID <= 0 || tabID > maxTabID {
		return invalid("no task or tab to record")
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO application_tab_opens (task_id, tab_id, opened_at) VALUES (?, ?, ?)`, taskID, tabID, now())
	return err
}

func (s Store) opened(ctx context.Context, taskID string, tabID int) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM application_tab_opens WHERE task_id = ? AND tab_id = ?`, taskID, tabID).Scan(&n)
	return n > 0, err
}
