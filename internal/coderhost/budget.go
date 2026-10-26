package coderhost

import (
	"errors"
	"fmt"
)

// A session with a budget (coder_sessions.budget_usd > 0) runs every turn under a spend cap of what
// it has left: its budget, less what its own turns spent, less what it carved out for its children.
// The spend is read from the turn journal (usage.updated events of the lead, the newest snapshot of
// each turn, plus the cost of finished org workers), so it is as good as the runtimes' own cost
// reports: a runtime that reports none counts as $0 spent, and the per-turn cap is still passed.

// BudgetLauncher is a Launcher that can start a turn under a spend cap (--budget-usd).
type BudgetLauncher interface {
	Launcher
	StartCapped(profileID, conversationID, turnID, message string, attachments []string, missionToken string, capUSD float64) (Turn, error)
}

// errBudgetUsedUp is why a send is refused when nothing is left.
var errBudgetUsedUp = errors.New("the session's budget is used up")

const spentSQL = `SELECT
  COALESCE((SELECT SUM(c) FROM (
     SELECT CAST(json_extract(e.payload, '$.costUsd') AS REAL) AS c FROM ai_chat_events e
      WHERE e.profile_id = ?1 AND e.conversation_id = ?2 AND e.type = 'usage.updated'
        AND COALESCE(json_extract(e.payload, '$.agentId'), '') = '' AND json_extract(e.payload, '$.costUsd') IS NOT NULL
        AND e.seq = (SELECT MAX(e2.seq) FROM ai_chat_events e2
                      WHERE e2.profile_id = e.profile_id AND e2.conversation_id = e.conversation_id AND e2.turn_id = e.turn_id
                        AND e2.type = 'usage.updated' AND COALESCE(json_extract(e2.payload, '$.agentId'), '') = ''
                        AND json_extract(e2.payload, '$.costUsd') IS NOT NULL))), 0)
 + COALESCE((SELECT SUM(CAST(json_extract(payload, '$.costUsd') AS REAL)) FROM ai_chat_events
      WHERE profile_id = ?1 AND conversation_id = ?2 AND type = 'agent.finished' AND json_extract(payload, '$.costUsd') IS NOT NULL), 0)`

// Spent is what the session's turns have cost, from the journal.
func (s *Store) Spent(profileID, conversationID string) (float64, error) {
	var v float64
	err := s.db.QueryRow(spentSQL, profileID, conversationID).Scan(&v)
	return v, err
}

// TurnCap is the spend cap the session's next turn runs under: capped is false for a session
// with no budget; a capped session with nothing left gets errBudgetUsedUp.
func (s *Store) TurnCap(profileID, conversationID string) (capUSD float64, capped bool, err error) {
	budget, err := s.Budget(profileID, conversationID)
	if err != nil || budget <= 0 {
		return 0, false, err
	}
	spent, err := s.Spent(profileID, conversationID)
	if err != nil {
		return 0, false, err
	}
	carved, err := s.Carved(profileID, conversationID)
	if err != nil {
		return 0, false, err
	}
	left := budget - carved - spent
	if left <= 1e-9 {
		return 0, true, fmt.Errorf("%w ($%.2f budget, $%.2f spent, $%.2f given to children)", errBudgetUsedUp, budget, spent, carved)
	}
	return left, true, nil
}
