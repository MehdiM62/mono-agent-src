package automation

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/action"
)

type fakeReplacer struct {
	mu    sync.Mutex
	calls []string
	got   []action.SelectorEntry
}

func (f *fakeReplacer) ReplaceSelector(id, key string, e action.SelectorEntry) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id+"/"+key)
	f.got = append(f.got, e)
	return WhereOverlay, nil
}

func (f *fakeReplacer) n() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func jevPick(label string, best string) action.JevPick {
	return action.JevPick{Label: label, Entry: action.SelectorEntry{Intent: "the Send button", Candidates: []action.SelectorCandidate{
		{CSS: best, Score: 0.9}, {XPath: "/html/body/button", Score: 0.25},
	}}}
}

func newRepairRecorder(t *testing.T, db *sql.DB, rep SelectorReplacer) *HealthRecorder {
	t.Helper()
	r := NewHealthRecorder(db, HealthOptions{Interval: time.Hour, Repairer: rep})
	t.Cleanup(func() { r.Close() })
	return r
}

func pickAndFlush(t *testing.T, r *HealthRecorder, p action.JevPick) {
	t.Helper()
	r.ObserveJevPick("acme", "send", p)
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
}

func proposal(t *testing.T, db *sql.DB) (RepairProposal, bool) {
	t.Helper()
	p, ok, err := LoadRepairProposal(db, "acme", "send")
	if err != nil {
		t.Fatal(err)
	}
	return p, ok
}

func TestJevRepairProposalLifecycle(t *testing.T) {
	db := healthTestDB(t)
	rep := &fakeReplacer{}
	r := newRepairRecorder(t, db, rep)

	pickAndFlush(t, r, jevPick("Send message", "#send"))
	p, ok := proposal(t, db)
	if !ok || p.Picks != 1 || p.Ready || p.Label != "Send message" || p.Intent != "the Send button" || p.Candidates[0].CSS != "#send" {
		t.Fatalf("after one pick: ok=%v %+v", ok, p)
	}
	first := p.FirstSeen

	pickAndFlush(t, r, jevPick("Send", "#send")) // same best candidate
	p, _ = proposal(t, db)
	if p.Picks != 2 || !p.Ready || p.Label != "Send" || !p.FirstSeen.Equal(first) {
		t.Fatalf("after two consistent picks: %+v", p)
	}
	// Auto-repair is off by default: nothing was applied.
	if rep.n() != 0 {
		t.Fatalf("auto-applied without the opt-in: %v", rep.calls)
	}
	all, err := LoadRepairProposals(db, "")
	if err != nil || len(all) != 1 || !all[0].Ready {
		t.Fatalf("LoadRepairProposals = %+v, %v", all, err)
	}
	if other, _ := LoadRepairProposals(db, "other"); len(other) != 0 {
		t.Errorf("filter by automation: %+v", other)
	}

	pickAndFlush(t, r, jevPick("Settings", "#settings")) // a different element
	p, _ = proposal(t, db)
	if p.Picks != 1 || p.Ready || p.Candidates[0].CSS != "#settings" {
		t.Fatalf("an inconsistent pick must start over: %+v", p)
	}
}

func TestAcceptRepairReplacesSelectorAndResetsHealth(t *testing.T) {
	db := healthTestDB(t)
	rep := &fakeReplacer{}
	r := newRepairRecorder(t, db, rep)
	r.ObserveSelector("acme", "send", -1, false, false)
	pickAndFlush(t, r, jevPick("Send message", "#send"))
	pickAndFlush(t, r, jevPick("Send message", "#send"))

	res, err := AcceptRepair(db, rep, "acme", "send", time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if rep.n() != 1 || rep.calls[0] != "acme/send" || res.Where != WhereOverlay || res.Picks != 2 {
		t.Fatalf("calls %v res %+v", rep.calls, res)
	}
	e := rep.got[0]
	if e.Intent != "the Send button" || e.VerifiedAt != "2026-10-13T00:00:00Z" || len(e.Candidates) != 2 || e.Candidates[0].CSS != "#send" {
		t.Errorf("entry = %+v", e)
	}
	if _, ok := proposal(t, db); ok {
		t.Error("proposal kept after accepting it")
	}
	h := healthRow(t, db, "acme", "send")
	if h.Fail != 0 || h.Recent != "" || h.RerecordedAt == nil {
		t.Errorf("health not reset: %+v", h)
	}
	if _, err := AcceptRepair(db, rep, "acme", "send", time.Now()); !errors.Is(err, ErrNoRepairProposal) {
		t.Errorf("second accept: %v", err)
	}
}

func TestAutoRepairAppliesReadyProposalWhenOptedIn(t *testing.T) {
	db := healthTestDB(t)
	if AutoRepairEnabled(db) {
		t.Fatal("auto-repair on by default")
	}
	if _, err := db.Exec(`INSERT INTO configs (name, config_data) VALUES (?, 'true')`, AutoRepairConfig); err != nil {
		t.Fatal(err)
	}
	if !AutoRepairEnabled(db) {
		t.Fatal("config set but auto-repair off")
	}
	rep := &fakeReplacer{}
	r := newRepairRecorder(t, db, rep)
	pickAndFlush(t, r, jevPick("Send message", "#send"))
	if rep.n() != 0 {
		t.Fatal("applied after one pick")
	}
	pickAndFlush(t, r, jevPick("Send message", "#send"))
	if rep.n() != 1 || rep.got[0].Candidates[0].CSS != "#send" {
		t.Fatalf("calls %v", rep.calls)
	}
	if _, ok := proposal(t, db); ok {
		t.Error("proposal kept after auto-repair")
	}
}

func TestResetSelectorHealthDropsRepairProposal(t *testing.T) {
	db := healthTestDB(t)
	r := newRepairRecorder(t, db, nil)
	pickAndFlush(t, r, jevPick("Send message", "#send"))
	if err := ResetSelectorHealth(db, "acme", "send"); err != nil {
		t.Fatal(err)
	}
	if _, ok := proposal(t, db); ok {
		t.Error("a re-record must drop the proposal")
	}
}

func TestJevHintPersists(t *testing.T) {
	db := healthTestDB(t)
	r := newRepairRecorder(t, db, nil)
	if _, ok := r.LoadJevHint("gemini/generate/s"); ok {
		t.Fatal("hint before any save")
	}
	r.SaveJevHint("gemini/generate/s", "Send message")
	if err := r.Flush(); err != nil {
		t.Fatal(err)
	}
	// A new recorder (a new process) reads it back.
	r2 := newRepairRecorder(t, db, nil)
	if got, ok := r2.LoadJevHint("gemini/generate/s"); !ok || got != "Send message" {
		t.Errorf("hint = %q, %v", got, ok)
	}
}
