package coderhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// A session with a budget (coder_sessions.budget_usd > 0) runs every turn under a spend cap of what
// it has left: its budget, less what its own turns spent, less what it carved out for its children.
// The per-turn --budget-usd is a ceiling for THAT turn, not a total: the total holds because every
// turn starts with what the earlier ones left.
//
// Spend is read from the turn journal the way internal/aitask/budget/ledger.go does: a
// usage.updated snapshot is a running total within its turn, so a turn's figure is the largest
// non-negative cost it reported (a lower final snapshot does not lower it), and a worker's
// agent.finished cost counts once per (turn, worker). A turn whose runtime reported NO cost does not
// count as $0: it counts as having spent the whole cap it ran under (turn caps are recorded when the
// turn starts), so a runtime that cannot report cost cannot spend a multiple of the budget.

// minCap is the smallest cap worth starting a turn under; less left is "used up".
const minCap = 1e-6 // the chat launcher keeps the same number (minTurnCap)

// BudgetLauncher is a Launcher that can start a turn under a spend cap (--budget-usd).
type BudgetLauncher interface {
	Launcher
	StartCapped(profileID, conversationID, turnID, message string, attachments []string, missionToken string, capUSD float64) (Turn, error)
}

// errBudgetUsedUp is why a send is refused when nothing is left.
var errBudgetUsedUp = errors.New("the session's budget is used up")

// RecordTurnCap notes the cap a turn started under.
func (s *Store) RecordTurnCap(profileID, conversationID, turnID string, capUSD float64) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO coder_session_turn_caps (profile_id, conversation_id, turn_id, cap_usd) VALUES (?, ?, ?, ?)`,
		profileID, conversationID, turnID, capUSD)
	return err
}

func validCost(p *float64) bool { return p != nil && *p >= 0 && !math.IsNaN(*p) && !math.IsInf(*p, 0) }

// Spent is what the session's turns have cost.
func (s *Store) Spent(profileID, conversationID string) (float64, error) {
	caps := map[string]float64{}
	rows, err := s.db.Query(`SELECT turn_id, cap_usd FROM coder_session_turn_caps WHERE profile_id = ? AND conversation_id = ?`, profileID, conversationID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var t string
		var c float64
		if err := rows.Scan(&t, &c); err != nil {
			rows.Close()
			return 0, err
		}
		caps[t] = c
	}
	rows.Close()

	lead := map[string]float64{}       // turn -> its largest reported total
	workers := map[[2]string]float64{} // (turn, worker) -> its largest finished cost
	ev, err := s.db.Query(`SELECT turn_id, type, payload FROM ai_chat_events WHERE profile_id = ? AND conversation_id = ? AND type IN ('usage.updated', 'agent.finished')`, profileID, conversationID)
	if err != nil {
		return 0, err
	}
	for ev.Next() {
		var turn, typ, raw string
		if err := ev.Scan(&turn, &typ, &raw); err != nil {
			ev.Close()
			return 0, err
		}
		var p struct {
			AgentID string   `json:"agentId"`
			CostUSD *float64 `json:"costUsd"`
		}
		if json.Unmarshal([]byte(raw), &p) != nil || !validCost(p.CostUSD) {
			continue // not numeric, negative: not a cost
		}
		if typ == "usage.updated" {
			if cur, seen := lead[turn]; p.AgentID == "" && (!seen || *p.CostUSD > cur) {
				lead[turn] = *p.CostUSD
			}
		} else if k := [2]string{turn, p.AgentID}; true {
			if cur, seen := workers[k]; !seen || *p.CostUSD > cur {
				workers[k] = *p.CostUSD
			}
		}
	}
	ev.Close()
	total := 0.0
	for turn, c := range caps {
		if v, ok := lead[turn]; ok {
			total += v
		} else {
			total += c // no cost reported: the whole cap
		}
		delete(lead, turn)
	}
	for _, v := range lead {
		total += v // turns that ran before the session had a budget
	}
	for _, v := range workers {
		total += v
	}
	return total, nil
}

// TurnCap is the spend cap the session's next turn runs under: capped is false for a session
// with no budget; a capped session with (almost) nothing left gets errBudgetUsedUp.
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
	if left < minCap {
		return 0, true, fmt.Errorf("%w ($%.2f budget, $%.2f spent, $%.2f given to children)", errBudgetUsedUp, budget, spent, carved)
	}
	return left, true, nil
}
