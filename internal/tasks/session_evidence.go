package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrNoEvidence is the refusal to hand a code task to review with nothing to check it by.
var ErrNoEvidence = errors.New("a code task needs evidence")

// evidenceKinds are the artifacts that count as evidence of work: a commit, a diff, test
// output, a file, a log. A report is the worker's own words about the work, so it does not.
var evidenceKinds = map[string]bool{"commit": true, "diff": true, "test": true, "file": true, "log": true}

const metaPrefix = "[mission-meta] "

// declaresFiles says whether a task's notes name the files it touches: in the planner's
// closing [mission-meta] line, or in a "Files:" block.
func declaresFiles(notes string) bool {
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(notes, "\n "), "\r\n", "\n"), "\n")
	if last := lines[len(lines)-1]; strings.HasPrefix(last, metaPrefix) {
		var m struct {
			Files []string `json:"files"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(last, metaPrefix)), &m) == nil {
			for _, f := range m.Files {
				if strings.TrimSpace(f) != "" {
					return true
				}
			}
		}
	}
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if len(t) < 6 || !strings.EqualFold(t[:6], "files:") {
			continue
		}
		if strings.TrimSpace(t[6:]) != "" {
			return true
		}
		if i+1 < len(lines) && strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(lines[i+1]), "-*+ \t")) != "" {
			return true
		}
	}
	return false
}

// needsEvidence reads the task and refuses a result for a task of a session that is a code
// task (kind task, files declared) and holds no evidence artifact. Research tasks (no
// files), checks, epics and questions are exempt.
func (s *Store) needsEvidence(ctx context.Context, x dbx, profileID string, id int64) error {
	t, err := s.getTx(ctx, x, profileID, id)
	if err != nil {
		return err
	}
	if (t.Kind != "" && t.Kind != KindTask) || !declaresFiles(t.Notes) {
		return nil
	}
	rows, err := x.QueryContext(ctx, `SELECT kind FROM task_artifacts WHERE task_id = ?`, id)
	if err != nil {
		return fmt.Errorf("tasks: reading the evidence of #%d: %w", id, err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return fmt.Errorf("tasks: reading the evidence of #%d: %w", id, err)
		}
		if evidenceKinds[k] {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w: task #%d declares files, so finishing it needs at least one artifact "+
		"(commit, diff, test, file or log): attach it with `task artifact %d --kind commit|diff|test --ref REF`, then finish again: %w",
		ErrInvalid, id, id, ErrNoEvidence)
}
