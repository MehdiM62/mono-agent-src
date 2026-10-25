package tasks

import (
	"errors"
	"testing"
)

// reviewed makes a running session with one task handed to Review by worker "w1".
func reviewed(t *testing.T, s *Store, worker Actor) (Session, Task) {
	t.Helper()
	se := mustSession(t, s, "default", "rev")
	tk := mustTask(t, s, "default", se.ID, "work")
	mustRun(t, s, "default", se)
	w := worker
	w.Session = se.ID
	if w.Task != 0 {
		w.Task = tk.ID
	}
	if _, err := s.SessionClaim(bg, "default", se.ID, tk.ID, w, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionFinish(bg, "default", se.ID, tk.ID, Outcome{Result: "did it"}, w); err != nil {
		t.Fatal(err)
	}
	return se, tk
}

func notVerified(t *testing.T, s *Store, id int64) {
	t.Helper()
	got, _, err := s.Get(bg, "default", id)
	if err != nil || got.Verified || got.Status != StatusReview {
		t.Fatalf("task changed: %+v %v", got, err)
	}
}

// Finding 1: a worker bound to the task cannot verify it under another name.
func TestBoundWorkerCannotVerifyItsTask(t *testing.T) {
	s, _, _ := newTestStore(t)
	se, tk := reviewed(t, s, Actor{Kind: Agent, Name: "w1", Task: 7})
	for _, name := range []string{"w1", "w2", "agent:x#ab12"} {
		w := Actor{Kind: Agent, Name: name, Session: se.ID, Task: tk.ID}
		if _, err := s.VerifySessionTask(bg, "default", se.ID, tk.ID, true, "", w); !errors.Is(err, ErrOperatorOnly) {
			t.Fatalf("%s: want a binding refusal, got %v", name, err)
		}
	}
	notVerified(t, s, tk.ID)
}

// Finding 1: the same agent identity (the MCP name carries #<4 hex>) cannot verify what it did.
func TestSameIdentityCannotVerify(t *testing.T) {
	cases := []struct{ worker, verifier string }{
		{"agent:claude#ab12", "agent:claude#cd34"},
		{"agent:claude", "agent:claude#cd34"},
		{"agent:claude#ab12", "agent:claude"},
		{"w1", "w1"},
		{"Agent:Claude#ab12", "agent:claude#ab12"},
	}
	for _, c := range cases {
		s, _, _ := newTestStore(t)
		se, tk := reviewed(t, s, Actor{Kind: Agent, Name: c.worker})
		v := Actor{Kind: Agent, Name: c.verifier, Session: se.ID}
		if _, err := s.VerifySessionTask(bg, "default", se.ID, tk.ID, true, "", v); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s verifying %s: %v", c.verifier, c.worker, err)
		}
		notVerified(t, s, tk.ID)
	}
	s, _, _ := newTestStore(t)
	se, tk := reviewed(t, s, Actor{Kind: Agent, Name: "agent:claude#ab12"})
	if _, err := s.VerifySessionTask(bg, "default", se.ID, tk.ID, true, "", Actor{Kind: Agent, Name: "agent:codex#cd34", Session: se.ID}); err != nil {
		t.Fatalf("another client verifies: %v", err)
	}
}

// Finding 1: a verifier bound to another task is refused; the operator may verify.
func TestVerifierOfAnotherTaskAndOperator(t *testing.T) {
	s, _, _ := newTestStore(t)
	se, tk := reviewed(t, s, Actor{Kind: Agent, Name: "w1"})
	v := Actor{Kind: Agent, Name: "v", Session: se.ID, Task: tk.ID + 1, Verifier: true}
	if _, err := s.VerifySessionTask(bg, "default", se.ID, tk.ID, true, "", v); !errors.Is(err, ErrOperatorOnly) {
		t.Fatalf("verifier of another task: %v", err)
	}
	notVerified(t, s, tk.ID)
	if got, err := s.VerifySessionTask(bg, "default", se.ID, tk.ID, true, "ok", human); err != nil || !got.Verified {
		t.Fatalf("operator: %+v %v", got, err)
	}
}

// Finding 3: an agent cannot verify while the session is paused; the operator can.
func TestVerifyRefusedWhilePaused(t *testing.T) {
	s, _, _ := newTestStore(t)
	se, tk := reviewed(t, s, Actor{Kind: Agent, Name: "w1"})
	if _, err := s.PauseSession(bg, "default", se.ID, human); err != nil {
		t.Fatal(err)
	}
	v := Actor{Kind: Agent, Name: "checker", Session: se.ID}
	if _, err := s.VerifySessionTask(bg, "default", se.ID, tk.ID, true, "", v); !errors.Is(err, ErrNotReady) {
		t.Fatalf("paused: %v", err)
	}
	if _, err := s.VerifySessionTask(bg, "default", se.ID, tk.ID, false, "bad", v); !errors.Is(err, ErrNotReady) {
		t.Fatalf("paused fail: %v", err)
	}
	notVerified(t, s, tk.ID)
	if _, err := s.ResumeSession(bg, "default", se.ID, human); err != nil {
		t.Fatal(err)
	}
	if got, err := s.VerifySessionTask(bg, "default", se.ID, tk.ID, true, "", v); err != nil || !got.Verified {
		t.Fatalf("resumed: %+v %v", got, err)
	}
}
