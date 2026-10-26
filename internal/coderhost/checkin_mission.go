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
