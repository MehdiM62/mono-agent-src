package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/aitask"
	"github.com/monoes/mono-agent/internal/applications"
)

// fakeSubmit stands in for the `aitask submit` process: it records the arguments and prints a task.
type fakeSubmit struct {
	calls [][]string
	out   string
	err   error
	conv  string
}

func (f *fakeSubmit) install(t *testing.T) {
	t.Helper()
	oldSubmit, oldOp := submitTask, FillStartWiring.RequireOperator
	t.Cleanup(func() { submitTask, FillStartWiring.RequireOperator = oldSubmit, oldOp })
	FillStartWiring.RequireOperator = func(string) error { return nil }
	submitTask = func(_ context.Context, args []string) ([]byte, error) {
		f.calls = append(f.calls, args)
		if f.err != nil {
			return []byte(f.out), f.err
		}
		return []byte(`{"taskId":"at-0123456789abcdef","kind":"application-apply","conversationId":"` + f.conv + `","status":"running"}`), nil
	}
}

func flagValues(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

func insertTask(t *testing.T, db *sql.DB, appID, status, result string) {
	t.Helper()
	s := &aitask.Store{DB: db, Profile: "default"}
	task, err := s.Insert(aitask.Task{ID: aitask.NewID(), Kind: "application-apply", ConversationID: "old-conv", Status: status,
		Goal: "g", ApplicationID: appID, Access: aitask.AccessWorkspace, BudgetUSD: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result != "" {
		if _, err := db.Exec(`UPDATE ai_tasks SET result_json = ? WHERE id = ?`, result, task.ID); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFillStartSubmitsTheKindWithItsThreeContextClasses(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	f := &fakeSubmit{conv: "conv-1"}
	f.install(t)

	out, err := run(t, dbPath, true, "fill", "start", id, "--runtime", "codex")
	if err != nil {
		t.Fatalf("fill start: %v (%s)", err, out)
	}
	if len(f.calls) != 1 {
		t.Fatalf("submit calls = %d", len(f.calls))
	}
	args := f.calls[0]
	joined := strings.Join(args, " ")
	for _, want := range []string{"--json aitask submit", "--kind application-apply", "--application " + id, "--runtime codex"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args lack %q: %s", want, joined)
		}
	}
	classes := []string{}
	for _, c := range flagValues(args, "--context") {
		classes = append(classes, strings.SplitN(c, "=", 2)[0])
	}
	if strings.Join(classes, ",") != "posting,facts" {
		t.Errorf("context classes = %v", classes)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["taskId"] != "at-0123456789abcdef" || got["conversationId"] != "conv-1" {
		t.Fatalf("output %q: %v", out, err)
	}
	withDB(t, dbPath, func(db *sql.DB) {
		store := applications.NewStore(db)
		links, _ := store.ListLinks(context.Background(), "default", id, applications.LinkConversation)
		if len(links) != 1 || links[0].RefID != "conv-1" {
			t.Errorf("conversation links = %+v", links)
		}
		fill, _ := store.GetFill(context.Background(), "default", id)
		if fill.Status != applications.FillFilling {
			t.Errorf("fill = %+v", fill)
		}
		app, _ := store.Get(context.Background(), "default", id)
		if app.Status != applications.StatusPending {
			t.Errorf("a started fill moved the status to %s", app.Status)
		}
	})
}

func TestFillStartIsOperatorOnly(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	f := &fakeSubmit{conv: "c"}
	f.install(t)
	FillStartWiring.RequireOperator = func(what string) error { return errors.New("only the user can " + what) }
	if _, err := run(t, dbPath, true, "fill", "start", id); err == nil || !strings.Contains(err.Error(), "only the user") {
		t.Fatalf("agent fill start: %v", err)
	}
	FillStartWiring.RequireOperator = nil // unwired: refused too
	if _, err := run(t, dbPath, true, "fill", "start", id); err == nil {
		t.Fatal("an unwired operator check let it through")
	}
	if len(f.calls) != 0 {
		t.Fatalf("submit ran %d times for a refused caller", len(f.calls))
	}
}

func TestFillStartOneOpenTaskPerApplication(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	f := &fakeSubmit{conv: "conv-2"}
	f.install(t)
	withDB(t, dbPath, func(db *sql.DB) { insertTask(t, db, id, aitask.StatusRunning, "") })
	out, err := run(t, dbPath, true, "fill", "start", id)
	if err == nil || !strings.Contains(err.Error(), "already has an open task") {
		t.Fatalf("second task: %v (%s)", err, out)
	}
	if len(f.calls) != 0 {
		t.Fatal("submitted despite an open task")
	}
	// Once the first is over, a new fill replaces the earlier conversation link.
	withDB(t, dbPath, func(db *sql.DB) {
		if _, err := db.Exec(`UPDATE ai_tasks SET status = 'cancelled'`); err != nil {
			t.Fatal(err)
		}
		if err := applications.NewStore(db).AddLink(context.Background(), "default", id, applications.LinkConversation, "old-conv"); err != nil {
			t.Fatal(err)
		}
	})
	if out, err := run(t, dbPath, true, "fill", "start", id); err != nil {
		t.Fatalf("after the first ended: %v (%s)", err, out)
	}
	withDB(t, dbPath, func(db *sql.DB) {
		links, _ := applications.NewStore(db).ListLinks(context.Background(), "default", id, applications.LinkConversation)
		if len(links) != 1 || links[0].RefID != "conv-2" {
			t.Errorf("links = %+v", links)
		}
	})
}

func TestFillStartCoderDisabledKeepsTheOneshotFallback(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	f := &fakeSubmit{out: `{"error":"coder mode is disabled","code":"coder_disabled","fallback":"oneshot"}` + "\n", err: errors.New("exit 3")}
	f.install(t)
	out, err := run(t, dbPath, true, "fill", "start", id)
	if err == nil {
		t.Fatal("no error")
	}
	var fe interface{ JSONErrorFields() map[string]any }
	if !errors.As(err, &fe) || fe.JSONErrorFields()["code"] != "coder_disabled" || fe.JSONErrorFields()["fallback"] != "oneshot" {
		t.Fatalf("err = %#v (%s)", err, out)
	}
	withDB(t, dbPath, func(db *sql.DB) {
		links, _ := applications.NewStore(db).ListLinks(context.Background(), "default", id, "")
		if len(links) != 0 {
			t.Errorf("a refused fill left links: %+v", links)
		}
	})
}

func TestFillStartRefusesAnApplicationThatIsOver(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	f := &fakeSubmit{conv: "c"}
	f.install(t)
	withDB(t, dbPath, func(db *sql.DB) {
		if err := applications.NewStore(db).SetStatus(context.Background(), "default", id, applications.StatusApplied, applications.ActorUser, "sent"); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := run(t, dbPath, true, "fill", "start", id); code(err) != 3 {
		t.Fatalf("applied application: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatal("submitted for an applied application")
	}
}

func TestFillSyncRecordsACompletedTask(t *testing.T) {
	dbPath := homeDB(t)
	id := addJob(t, dbPath)
	withDB(t, dbPath, func(db *sql.DB) { insertTask(t, db, id, aitask.StatusRunning, "") })
	out, err := run(t, dbPath, true, "fill", "sync", id)
	if err != nil || !strings.Contains(out, `"recorded": false`) {
		t.Fatalf("running task: %v (%s)", err, out)
	}
	withDB(t, dbPath, func(db *sql.DB) {
		if _, err := db.Exec(`UPDATE ai_tasks SET status = 'complete', result_json = ?`,
			`{"status":"filled","fields":[{"label":"Email","value":"a@b.example","source":"Profile info","generated":false}],"note":"ok"}`); err != nil {
			t.Fatal(err)
		}
	})
	if out, err := run(t, dbPath, true, "fill", "sync", id); err != nil || !strings.Contains(out, `"recorded": true`) {
		t.Fatalf("complete task: %v (%s)", err, out)
	}
	withDB(t, dbPath, func(db *sql.DB) {
		store := applications.NewStore(db)
		app, _ := store.Get(context.Background(), "default", id)
		fill, _ := store.GetFill(context.Background(), "default", id)
		if app.Status != applications.StatusAwaitingSubmit || fill.Status != applications.FillFilled || len(fill.Fields) != 1 {
			t.Errorf("status %s, fill %+v", app.Status, fill)
		}
	})
}
