package applyfill

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/applications"
	"github.com/monoes/mono-agent/internal/profiledir"
)

// Result is the validated result of an application-apply task.
type Result struct {
	Status     string  `json:"status"`
	Screenshot string  `json:"screenshot"`
	Fields     []Field `json:"fields"`
	Note       string  `json:"note"`
}

// ErrNotFinished: the task has no result to record yet.
var ErrNotFinished = errors.New("the application-apply task has not completed")

// Finalize records a completed application-apply task on its application:
// the fill record from the validated result, and the application's status
// (filled: awaiting_submit, needs_you: needs_you). It is the completion
// hook of the kind; running it twice records the same thing once. It never
// sets applied (the user's) and never moves an application that is already
// applied, rejected or cancelled.
func Finalize(ctx context.Context, db *sql.DB, profile string, t aitask.Task) error {
	if t.Kind != Kind || t.ApplicationID == "" {
		return fmt.Errorf("task %s is not an %s task for an application", t.ID, Kind)
	}
	if t.Status != aitask.StatusComplete || len(t.Result) == 0 {
		return ErrNotFinished
	}
	var res Result
	if err := json.Unmarshal(t.Result, &res); err != nil {
		return fmt.Errorf("task %s: reading its result: %w", t.ID, err)
	}
	if err := aitask.Validate(Schema, mustDecode(t.Result)); err != nil {
		return fmt.Errorf("task %s: %w", t.ID, err)
	}
	fill, to := recordFor(db, profile, res)
	store := applications.NewStore(db)
	if err := store.RecordFill(ctx, profile, t.ApplicationID, fill); err != nil {
		return err
	}
	app, err := store.Get(ctx, profile, t.ApplicationID)
	if err != nil {
		return err
	}
	if app.Status == to || !canMove(app.Status) {
		return nil
	}
	return store.SetStatus(ctx, profile, t.ApplicationID, to, applications.ActorSystem, "form fill by task "+t.ID)
}

func mustDecode(raw []byte) any {
	var v any
	_ = json.Unmarshal(raw, &v)
	return v
}

// canMove: only an application still being worked on is moved by a fill.
func canMove(s applications.Status) bool {
	return s == applications.StatusPending || s == applications.StatusAwaitingSubmit || s == applications.StatusNeedsYou
}

// recordFor turns a result into the fill record and the status it asks for.
// Fields that must not have been filled are dropped and counted in the note;
// everything is cut to the record's caps; a screenshot outside the profile
// folder is not kept.
func recordFor(db *sql.DB, profile string, res Result) (applications.Fill, applications.Status) {
	fields, dropped := Vet(res.Fields)
	note := cut(res.Note, maxNote-120)
	if dropped > 0 {
		note = strings.TrimSpace(fmt.Sprintf("%s (%d field(s) that must never be filled were left out of this record.)", note, dropped))
	}
	f := applications.Fill{Status: applications.FillFilled, Note: cut(note, maxNote)}
	to := applications.StatusAwaitingSubmit
	if res.Status == StatusNeedsYou {
		f.Status, to = applications.FillNeedsYou, applications.StatusNeedsYou
	}
	for i, x := range fields {
		if i == maxFields {
			break
		}
		label := cut(strings.TrimSpace(x.Label), maxLabel)
		if label == "" {
			continue
		}
		f.Fields = append(f.Fields, applications.FillField{Label: label, Value: cut(x.Value, maxValue),
			Source: cut(x.Source, maxSource), Generated: x.Generated})
	}
	if p, err := insideFolder(profiledir.Root(db, profile), res.Screenshot); err == nil {
		f.Screenshot = p
	}
	return f, to
}

// cut shortens s to at most n bytes without splitting a character.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && s != "" {
		s = s[:len(s)-1]
	}
	return s
}

// insideFolder resolves path (symlinks included) and returns it only when it
// is an existing regular file inside root.
func insideFolder(root, path string) (string, error) {
	if root == "" || path == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("no file")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("outside the profile folder")
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	return real, nil
}
