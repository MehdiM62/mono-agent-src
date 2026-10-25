package dynorg

import (
	"context"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/tasks"
)

// silentVerifiers makes the rig's verifiers end without recording a verdict.
func (r *missionRig) silentVerifiers() {
	r.ex.verify = func(ctx context.Context, id int64, o monomind.ExecOptions) (*monomind.TurnResult, error) {
		return &monomind.TurnResult{SawDone: true, ResultText: "looked, no verdict", StopReason: monomind.StopEndTurn}, nil
	}
}

func (r *missionRig) noVerdictComments(id int64) int {
	d, _ := r.store.SessionTask(tbg, "default", r.sid, id, operator)
	n, _ := verifierEndings(d, r.sid)
	return n
}

func (r *missionRig) verifierRuns(id int64) int {
	r.ex.mu.Lock()
	defer r.ex.mu.Unlock()
	return len(r.ex.vruns[id])
}

func TestMissionVerifierWithoutVerdictIsRetriedThenBlocked(t *testing.T) {
	r := newRig(t, rigOpts{})
	a := r.add(spec{title: "a", files: "a.go"})
	r.approve()
	r.silentVerifiers()
	r.toReview(a.ID)
	if _, err := r.verify(a.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the first ending", func() bool { return r.noVerdictComments(a.ID) == 1 && !r.c.verifierLive(a.ID) })

	// mission_next starts a second verifier on its own
	res := r.next(r.c)
	if !strings.Contains(res.Note, "new one started") {
		t.Fatalf("note %q", res.Note)
	}
	eventually(t, "the second ending", func() bool { return r.noVerdictComments(a.ID) == 2 && !r.c.verifierLive(a.ID) })
	if n := r.verifierRuns(a.ID); n != 2 {
		t.Fatalf("verifier runs %d", n)
	}

	// at the cap the task is blocked for the operator; no third run
	r.next(r.c)
	tk := r.task(a.ID)
	if !tk.Blocked || tk.Status != tasks.StatusReview || r.verifierRuns(a.ID) != 2 {
		t.Fatalf("after the cap: %+v, runs %d", tk, r.verifierRuns(a.ID))
	}
	d, _ := r.store.SessionTask(tbg, "default", r.sid, a.ID, operator)
	if notes := strings.Join(failureNotes(d), "|"); !strings.Contains(notes, noVerdictBlockNote) {
		t.Fatalf("failure notes %q", notes)
	}
	v := r.finish()
	if v.Finished || len(v.Unfinished) != 1 || v.Unfinished[0].ID != a.ID || !v.Unfinished[0].Blocked {
		t.Fatalf("a blocked task is unfinished: %+v", v)
	}
	if _, err := r.verify(a.ID); err == nil {
		t.Fatal("a blocked task started another verifier")
	}
}

func TestMissionVerifierAttemptCountSurvivesANewConductor(t *testing.T) {
	r := newRig(t, rigOpts{})
	a := r.add(spec{title: "a", files: "a.go"})
	r.approve()
	r.silentVerifiers()
	r.toReview(a.ID)
	if _, err := r.verify(a.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the first ending", func() bool { return r.noVerdictComments(a.ID) == 1 && !r.c.verifierLive(a.ID) })

	c2 := r.conductor(Limits{MaxAgents: 20, MaxConcurrent: 5}) // a restarted turn
	r.next(c2)
	eventually(t, "the second ending", func() bool { return r.noVerdictComments(a.ID) == 2 && !c2.verifierLive(a.ID) })
	c3 := r.conductor(Limits{MaxAgents: 20, MaxConcurrent: 5})
	r.next(c3)
	if tk := r.task(a.ID); !tk.Blocked || r.verifierRuns(a.ID) != 2 {
		t.Fatalf("the count did not carry over: %+v, runs %d", tk, r.verifierRuns(a.ID))
	}
}

func TestMissionVerdictStopsVerifierRetries(t *testing.T) {
	for _, pass := range []bool{true, false} {
		r := newRig(t, rigOpts{})
		a := r.add(spec{title: "a", files: "a.go"})
		r.approve()
		r.silentVerifiers()
		r.toReview(a.ID)
		if _, err := r.verify(a.ID); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the first ending", func() bool { return r.noVerdictComments(a.ID) == 1 && !r.c.verifierLive(a.ID) })
		r.verdicts(func(int64) (bool, string) { return pass, "criterion 1 not met" })
		r.next(r.c)
		if pass {
			eventually(t, "verified", func() bool { return r.task(a.ID).Verified })
		} else {
			r.waitStatus(a.ID, tasks.StatusReady)
		}
		eventually(t, "the verifier to end", func() bool { return !r.c.verifierLive(a.ID) })
		runs := r.verifierRuns(a.ID)
		r.next(r.c)
		r.next(r.c)
		if r.verifierRuns(a.ID) != runs || runs != 2 {
			t.Fatalf("pass=%v: verifier runs %d then %d: a verdict stops the retries", pass, runs, r.verifierRuns(a.ID))
		}
		if tk := r.task(a.ID); tk.Blocked {
			t.Fatalf("pass=%v: blocked after a verdict: %+v", pass, tk)
		}
	}
}
