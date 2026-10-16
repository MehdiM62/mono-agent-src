package tasks

import (
	"context"
	"fmt"
)

// attachAuthors fills Author of every task from its created event, with one
// query.
func (s *Store) attachAuthors(ctx context.Context, x dbx, ts []Task) error {
	if len(ts) == 0 {
		return nil
	}
	index := make(map[int64]int, len(ts))
	args := make([]any, 0, len(ts))
	for i, t := range ts {
		index[t.ID] = i
		args = append(args, t.ID)
	}
	rows, err := x.QueryContext(ctx,
		`SELECT task_id, actor FROM task_events WHERE kind = 'created' AND task_id IN (`+placeholders(len(ts))+`) ORDER BY id`, args...)
	if err != nil {
		return fmt.Errorf("tasks: reading authors: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var actor string
		if err := rows.Scan(&id, &actor); err != nil {
			return fmt.Errorf("tasks: reading authors: %w", err)
		}
		if t := &ts[index[id]]; t.Author == "" {
			t.Author = actor
		}
	}
	return rows.Err()
}
