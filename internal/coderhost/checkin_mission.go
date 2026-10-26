package coderhost

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/tasks"
)

// missionFact is the progress of the task session a master's conversation is
// working (a mission), as the check-in digest shows it. Like the rest of the
// digest it is the host's own record: a state word and counts. The session's
// title, goal and every task's text are agent-written and never reach the digest.
type missionFact struct {
	State    string `json:"state"`
	Done     int    `json:"done"`
	Review   int    `json:"review"`
	Total    int    `json:"total"`
	Verified int    `json:"verified"`
	Blocked  int    `json:"blocked"`
}

// hostReader reads the task tables for the digest. It only reads: the digest
// grants nothing and writes nothing to a session.
var hostReader = tasks.Actor{Kind: tasks.Human, Name: "coder-host"}

// missionOf is the open (draft, running or paused) task session bound to the
// conversation, or nil. ok is false when the read failed (the caller keeps what it had).
func (h *Host) missionOf(profileID, conversationID string) (m *missionFact, ok bool) {
	list, err := tasks.NewStore(h.Store.db).ListSessions(context.Background(), profileID, tasks.SessionFilter{}, hostReader)
	if err != nil {
		return nil, false
	}
	for _, se := range list { // newest first
		if se.ConversationID != conversationID {
			continue
		}
		switch se.State {
		case tasks.SessionDraft, tasks.SessionRunning, tasks.SessionPaused:
			return &missionFact{State: string(se.State), Done: se.Counts.Done, Review: se.Counts.Review, Total: se.Counts.Total,
				Verified: se.Counts.Verified, Blocked: se.Counts.Blocked}, true
		}
	}
	return nil, true
}

// missionLine is the digest's line for a mission: numbers and a state word only.
func missionLine(m *missionFact) string {
	if m == nil {
		return ""
	}
	return fmt.Sprintf("Mission (your task session): %s; %d of %d task(s) done, %d in review, %d verified, %d blocked. Closing tasks and approving a plan stay the operator's.\n",
		m.State, m.Done, m.Total, m.Review, m.Verified, m.Blocked)
}

// open says whether a running mission still has work to chase: a task neither done nor verified, a
// blocked one, or one in review that no verifier has passed. A verified task only waits for the operator
// to close it, which nothing a lead does can hasten.
func (m *missionFact) open() bool {
	return m != nil && m.State == string(tasks.SessionRunning) && (m.Blocked > 0 || m.Review > m.Verified || m.Done+m.Verified < m.Total)
}

// leadWithOpenWork is the rule that lets a session without children be checked in on: its
// conversation is bound to a RUNNING task session (not draft, not paused) that has open work. A draft
// session waits for the person's plan approval and a paused one for the person to resume it; neither is
// woken. A failed read is "no": nothing is queued on a guess.
func (h *Host) leadWithOpenWork(profileID, conversationID string) bool {
	m, ok := h.missionOf(profileID, conversationID)
	return ok && m.open()
}

// liveChildren counts the host-made children of a session that are not stopped, as the check-in pass sees them.
func (h *Host) liveChildren(profileID, conversationID string) (int, error) {
	list, err := h.Store.ListSessions("")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, x := range list {
		if x.ProfileID == profileID && x.Parent == conversationID && x.State != StateStopped && hostID.MatchString(x.ConversationID) {
			n++
		}
	}
	return n, nil
}

// checkinTargetStillValid is the one rule for who may be checked in on, used when a check-in is looked at,
// when it is about to be queued and when it is about to run. It returns why not, or "". The session
// must be unattended and not a child; with live children it must be a master; with none (a lead) its
// conversation must be bound to a RUNNING task session with open work.
func (h *Host) checkinTargetStillValid(x Session, liveChildren int) string {
	switch {
	case !x.Unattended:
		return "the session is no longer an unattended master"
	case x.Role == "child":
		return "the session is no longer an unattended master"
	case liveChildren > 0:
		if x.Role != "master" {
			return "the session is no longer an unattended master"
		}
	case !h.leadWithOpenWork(x.ProfileID, x.ConversationID):
		return "the session has no running mission with open work"
	}
	return ""
}
