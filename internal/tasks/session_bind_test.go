package tasks

import (
	"errors"
	"testing"
)

// A mission worker's token names its session and its task: it works that task and nothing
// else, even inside its own session.
func TestActorBoundToATaskTouchesOnlyThatTask(t *testing.T) {
	s, db, _ := newTestStore(t)
	se, ts := runningWith(t, s, "mine", "theirs")
	mine, theirs := ts[0], ts[1]
	worker := Actor{Kind: Agent, Name: "w1", Session: se.ID, Task: mine.ID}

	if _, err := s.SessionClaim(bg, "default", se.ID, theirs.ID, worker, 0); !errors.Is(err, ErrOperatorOnly) {
		t.Fatalf("claim of another task of its own session: %v", err)
	}
	if tk, _, _ := s.Get(bg, "default", theirs.ID); tk.Status != StatusReady || tk.Claim != nil {
		t.Fatalf("the refused claim changed the other task: %+v", tk)
	}
	if got, err := s.SessionNext(bg, "default", se.ID, worker, true, 0); !errors.Is(err, ErrOperatorOnly) || got != nil {
		t.Fatalf("a bound worker takes what it is given, not the next: %v %v", got, err)
	}
	if _, err := s.AddSessionTask(bg, "default", se.ID, SessionTaskInput{Title: "extra", Acceptance: "x"}, worker); !errors.Is(err, ErrOperatorOnly) {
		t.Fatalf("a bound worker adds no task: %v", err)
	}
	if n := countWhere(t, db, "tasks", "session_id = ?", se.ID); n != 2 {
		t.Fatalf("tasks in the session = %d, want 2", n)
	}

	// someone else holds `theirs`: the worker cannot finish it, nor comment on it
	if _, err := s.SessionClaim(bg, "default", se.ID, theirs.ID, bot("other"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionFinish(bg, "default", se.ID, theirs.ID, Outcome{Result: "stolen"}, worker); !errors.Is(err, ErrOperatorOnly) {
		t.Fatalf("finish of another task: %v", err)
	}
	if _, err := s.SessionComment(bg, "default", se.ID, theirs.ID, "hi", worker); !errors.Is(err, ErrOperatorOnly) {
		t.Fatalf("comment on another task: %v", err)
	}
	if tk, _, _ := s.Get(bg, "default", theirs.ID); tk.Status != StatusInProgress {
		t.Fatalf("the other task moved: %+v", tk)
	}

	// its own task it can claim, comment on, attach to and finish
	if _, err := s.SessionClaim(bg, "default", se.ID, mine.ID, worker, 0); err != nil {
		t.Fatalf("claim own: %v", err)
	}
	if _, err := s.AddArtifact(bg, "default", se.ID, mine.ID, ArtifactInput{Kind: "report", Ref: "r"}, worker); err != nil {
		t.Fatalf("artifact own: %v", err)
	}
	if tk, err := s.SessionFinish(bg, "default", se.ID, mine.ID, Outcome{Result: "done"}, worker); err != nil || tk.Status != StatusReview {
		t.Fatalf("finish own: %+v %v", tk, err)
	}
}
