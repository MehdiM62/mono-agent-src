package coderhost

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/ai"
)

func capRows(t *testing.T, st *Store, conv string) int {
	t.Helper()
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM coder_session_turn_caps WHERE conversation_id = ?`, conv).Scan(&n)
	return n
}

func TestATurnsCapIsOnRecordBeforeItStartsAndDroppedIfItDoesNotStart(t *testing.T) {
	h, l, st := newHost(t)
	m, _ := master(t, st, "coding")
	c := m.ConversationID
	st.SetBudget("p", c, 4)
	seen := -1
	l.onStart = func() { seen = capRows(t, st, c) }
	send(t, st, c, "t-1", "go")
	_ = h.Tick()
	if seen != 1 {
		t.Fatalf("cap rows when the turn started = %d, want 1", seen)
	}
	l.turn("t-1").end()
	waitFor(t, "settle", func() bool { return state(t, st, c).State == StateIdle })
	st.db.Exec(`UPDATE coder_session_turn_caps SET cap_usd = 0`) // forget what turn 1 held
	l.startErr = errors.New("no runtime")
	cmd := send(t, st, c, "t-2", "again")
	_ = h.Tick()
	if got, _, _ := st.GetCommand(cmd.ID); got.Status != CmdFailed {
		t.Fatalf("command = %+v", got)
	}
	if capRows(t, st, c) != 1 {
		t.Fatalf("a turn that did not start left its cap on record: %d rows", capRows(t, st, c))
	}
	// a cap that cannot be recorded stops the start
	l.startErr = nil
	st.db.Exec(`ALTER TABLE coder_session_turn_caps RENAME TO caps_x`)
	st.db.Exec(`UPDATE coder_session_commands SET status = 'done'`)
	n := len(l.started)
	cmd = send(t, st, c, "t-3", "third")
	_ = h.Tick()
	if got, _, _ := st.GetCommand(cmd.ID); got.Status != CmdFailed || len(l.started) != n {
		t.Fatalf("a turn started with no cap on record: %+v", got)
	}
}

func TestAFinalCostOfZeroUnderACapCountsAsUnreported(t *testing.T) {
	st := newStoreT(t)
	m, _ := master(t, st, "coding")
	c := m.ConversationID
	st.SetBudget("p", c, 5)
	addTurn(t, st, c, "t1", "completed", "x", time.Now().Add(-time.Hour))
	st.RecordTurnCap("p", c, "t1", 5)
	event(t, st, c, "t1", 1, "usage.updated", `{"agentId":"","costUsd":0}`)
	if spent, _ := st.Spent("p", c); spent != 5 {
		t.Fatalf("a zero under a cap is unreported and counts as the cap: spent = %v", spent)
	}
	event(t, st, c, "t1", 2, "usage.updated", `{"agentId":"","costUsd":1.5}`)
	if spent, _ := st.Spent("p", c); spent != 1.5 {
		t.Fatalf("a reported cost stands: spent = %v", spent)
	}
}

func TestTheRunningTurnsCapIsNotSpendButLimitsACarveMadeDuringIt(t *testing.T) {
	st := newStoreT(t)
	m, _ := master(t, st, "coding")
	c := m.ConversationID
	st.SetBudget("p", c, 10)
	running(t, st, c, "t-now")
	st.RecordTurnCap("p", c, "t-now", 10) // the running turn may spend all of it
	if spent, _ := st.Spent("p", c); spent != 0 {
		t.Fatalf("the in-flight turn's cap is charged as spend: %v", spent)
	}
	if _, err := st.Spawn(m, SpawnRequest{BudgetUSD: 1}); !errors.Is(err, ErrLimit) {
		t.Fatalf("a carve during a turn that may spend everything: %v", err)
	}
	// a turn that holds only part of it leaves the rest to carve
	st.db.Exec(`UPDATE coder_session_turn_caps SET cap_usd = 3`)
	if _, err := st.Spawn(m, SpawnRequest{BudgetUSD: 8}); !errors.Is(err, ErrLimit) {
		t.Fatalf("8 of the 7 left: %v", err)
	}
	if _, err := st.Spawn(m, SpawnRequest{BudgetUSD: 7}); err != nil {
		t.Fatalf("the 7 left: %v", err)
	}
	// once the turn ends it is charged as spend (no cost reported: its whole cap) and no longer reserved
	x := state(t, st, c)
	x.State, x.TurnID = StateIdle, ""
	st.SaveSession(x)
	if spent, _ := st.Spent("p", c); spent != 3 {
		t.Fatalf("an ended cost-less turn counts as its cap: %v", spent)
	}
}

func TestDeletingAConversationTakesItsBudgetTokenAndSpawnRowsAndSweepsOrphans(t *testing.T) {
	st := newStoreT(t)
	m, _ := master(t, st, "coding")
	a := spawn(t, st, m)
	running(t, st, a, "t1")
	mint(t, st, a, "t1")
	st.RecordTurnCap("p", a, "t1", 1)
	count := func(q string, args ...any) int {
		var n int
		st.db.QueryRow(q, args...).Scan(&n)
		return n
	}
	store, _ := ai.NewAIStore(st.db)
	x := state2(t, st, a)
	x.State, x.TurnID = StateIdle, ""
	st.SaveSession(x)
	if err := store.DeleteConversation(a, "p"); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT COUNT(*) FROM coder_session_turn_caps WHERE conversation_id = ?`, a)+count(`SELECT COUNT(*) FROM coder_session_tokens WHERE conversation_id = ?`, a) != 0 {
		t.Fatal("a deleted conversation left cap or token rows")
	}
	if count(`SELECT COUNT(*) FROM coder_session_spawns WHERE parent = ?`, m.ConversationID) != 1 {
		t.Fatal("a deleted child must keep its master's spawn row, or spawn-and-delete outruns the rate limit")
	}
	if err := store.DeleteConversation(m.ConversationID, "p"); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT COUNT(*) FROM coder_session_spawns WHERE parent = ?`, m.ConversationID) != 0 {
		t.Fatal("a deleted master left its spawn rows")
	}
	// rows of conversations that are gone, left by a racing writer, are swept
	st.db.Exec(`INSERT INTO coder_session_turn_caps VALUES ('p', 'ghost', 't', 1)`)
	st.db.Exec(`INSERT INTO coder_session_tokens (token_hash, profile_id, conversation_id, turn_id, created_at, expires_at) VALUES ('h', 'p', 'ghost', 't', 'x', 'y')`)
	st.db.Exec(`INSERT INTO coder_session_spawns (profile_id, parent, child, created_at) VALUES ('p', 'ghost', '', 'x')`)
	if _, err := st.SweepOrphans(); err != nil {
		t.Fatal(err)
	}
	if count(`SELECT (SELECT COUNT(*) FROM coder_session_turn_caps) + (SELECT COUNT(*) FROM coder_session_tokens) + (SELECT COUNT(*) FROM coder_session_spawns WHERE parent = 'ghost')`) != 0 {
		t.Fatal("orphan rows survived the sweep")
	}
}

func state2(t *testing.T, st *Store, conv string) Session {
	t.Helper()
	x, _, _ := st.GetSession("p", conv)
	return x
}

func TestHostInsideTurnIgnoresExpiredRowsAndAStaleStamp(t *testing.T) {
	st := newStoreT(t)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	st.now = func() time.Time { return now }
	m, _ := master(t, st, "coding")
	running(t, st, m.ConversationID, "t1")
	st.IssueSessionToken("p", m.ConversationID, "t1")
	pid := os.Getppid()
	st.BindSessionToken("p", m.ConversationID, "t1", pid, ProcStamp(pid))
	if !st.HostInsideTurn() {
		t.Fatal("a host below a live bound turn was not noticed")
	}
	if ancestrySupported {
		st.db.Exec(`UPDATE coder_session_tokens SET stamp = 'start:1'`)
		if st.HostInsideTurn() {
			t.Fatal("a pid that now belongs to another process counts")
		}
		st.db.Exec(`UPDATE coder_session_tokens SET stamp = ?`, ProcStamp(pid))
	}
	now = now.Add(SessionTokenTTL + time.Minute)
	if st.HostInsideTurn() {
		t.Fatal("an expired token row counts")
	}
}

func TestAHeldMasterMessageDoesNotHoldUpAPersonsMessage(t *testing.T) {
	h, l, st := newHost(t)
	m, _ := master(t, st, "coding")
	a := spawn(t, st, m)
	x := state(t, st, m.ConversationID)
	x.State = StatePaused
	st.SaveSession(x)
	held, _ := st.SendToChild(m, a, "from the paused master")
	person := send(t, st, a, "person-1", "from the user")
	_ = h.Tick()
	if got, _, _ := st.GetCommand(held.ID); got.Status != CmdPending {
		t.Fatalf("the master's message = %+v, want held", got)
	}
	if got, _, _ := st.GetCommand(person.ID); got.Status != CmdDone || len(l.started) != 1 || !strings.HasSuffix(l.started[0], "from the user") {
		t.Fatalf("the person's message = %+v started = %v", got, l.started)
	}
}

func TestAMasterAlwaysHasRoomToStopAChild(t *testing.T) {
	st := newStoreT(t)
	m, _ := master(t, st, "coding")
	a := spawn(t, st, m)
	for i := 0; i < MaxPendingMaster; i++ {
		if _, err := st.SendToChild(m, a, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ControlChild(m, a, KindPause); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("a pause behind a full master queue: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := st.ControlChild(m, a, KindStop); err != nil {
			t.Fatalf("stop %d: %v", i, err)
		}
	}
	if _, err := st.SendToChild(m, a, "one too many"); !errors.Is(err, ErrQueueFull) {
		t.Fatal("stops must not free or take master send slots")
	}
}

// answer is an issuer-tagged send whose turn id is not master-prefixed (another path's, e.g. an answer
// note a master gives a child): the shared issuer rules apply to it as to a master- send.
func answer(t *testing.T, st *Store, master, child, turn string) Command {
	t.Helper()
	c, err := st.Enqueue(Command{ProfileID: "p", ConversationID: child, Kind: KindSend, TurnID: turn, Message: "note", Issuer: master})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAnyIssuerTaggedSendFollowsTheMasterRules(t *testing.T) {
	h, l, st := newHost(t)
	m, _ := master(t, st, "coding")
	a := spawn(t, st, m)
	cmd := answer(t, st, m.ConversationID, a, "answer-m-1")
	// the helper's verdicts
	x := state(t, st, a)
	if ok, hold, reason := h.issuerStillAllowed(cmd, x); !ok || hold || reason != "" {
		t.Fatalf("live master: %v %v %q", ok, hold, reason)
	}
	if ok, _, _ := h.issuerStillAllowed(Command{ProfileID: "p", ConversationID: a}, x); !ok {
		t.Fatal("a command with no issuer is always allowed")
	}
	mx := state(t, st, m.ConversationID)
	mx.State = StatePaused
	st.SaveSession(mx)
	if ok, hold, _ := h.issuerStillAllowed(cmd, x); ok || !hold {
		t.Fatalf("paused master: ok=%v hold=%v", ok, hold)
	}
	if ok, hold, _ := h.issuerStillAllowed(Command{ProfileID: "p", ConversationID: a, Kind: KindStop, Issuer: m.ConversationID}, x); !ok || hold {
		t.Fatalf("a paused master can still stop its child: ok=%v hold=%v", ok, hold)
	}
	_ = h.Tick()
	if got, _, _ := st.GetCommand(cmd.ID); got.Status != CmdPending || len(l.started) != 0 {
		t.Fatalf("held send = %+v", got)
	}
	// a stop of the master cancels it
	mx.State = StateIdle
	st.SaveSession(mx)
	ca := state(t, st, a)
	ca.State = StatePaused
	st.SaveSession(ca)
	if _, err := st.Enqueue(Command{ProfileID: "p", ConversationID: m.ConversationID, Kind: KindStop}); err != nil {
		t.Fatal(err)
	}
	_ = h.Tick()
	if got, _, _ := st.GetCommand(cmd.ID); got.Status != CmdFailed || got.Error != "the master was stopped" {
		t.Fatalf("after the master's stop: %+v", got)
	}
	// a gone master
	mx2, _ := master(t, st, "coding")
	b := spawn(t, st, mx2)
	gone := answer(t, st, mx2.ConversationID, b, "answer-m-2")
	st.db.Exec(`UPDATE coder_sessions SET state = 'stopped' WHERE conversation_id = ?`, mx2.ConversationID)
	_ = h.Tick()
	if got, _, _ := st.GetCommand(gone.ID); got.Status != CmdFailed || !strings.Contains(got.Error, "gone or stopped") {
		t.Fatalf("stopped master: %+v", got)
	}
}

func TestAnyIssuerTaggedSendExpiresLikeACheckin(t *testing.T) {
	st := newStoreT(t)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	st.now = func() time.Time { return now }
	m, _ := master(t, st, "coding")
	a := spawn(t, st, m)
	answer(t, st, m.ConversationID, a, "answer-m-1")
	st.Enqueue(Command{ProfileID: "p", ConversationID: a, Kind: KindSend, TurnID: "person-1", Message: "hi"})
	now = now.Add(MaxCheckinAge + time.Minute)
	st.ExpireSends()
	pend, _ := st.Pending()
	if len(pend) != 1 || pend[0].TurnID != "person-1" {
		t.Fatalf("pending = %+v", pend)
	}
}
