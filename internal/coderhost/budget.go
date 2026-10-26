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

// Mid-turn rules. A turn's cap is recorded before the turn starts. Its figure is the largest cost it
// reported; a final cost of exactly 0 (or none) under a positive cap counts as UNREPORTED, and an
// unreported turn counts as its whole cap (there is no separate "reported" marker in the journal, so a
// genuinely free turn is charged its cap: the safe side). The turn running RIGHT NOW is the exception for
// Spent: until it ends, its cap is not charged as spend (so a master with a cost-less running turn is not
// shown ~$0 left), but a carve made during it subtracts that turn's cap (carveReserve), because the turn
// may still spend up to it: budget - spent - carved - running cap, so the total cannot reach 2x.

// RecordTurnCap notes the cap a turn started under.
func (s *Store) RecordTurnCap(profileID, conversationID, turnID string, capUSD float64) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO coder_session_turn_caps (profile_id, conversation_id, turn_id, cap_usd) VALUES (?, ?, ?, ?)`,
		profileID, conversationID, turnID, capUSD)
	return err
}

// DropTurnCap forgets the cap of a turn that did not start.
func (s *Store) DropTurnCap(profileID, conversationID, turnID string) {
	_, _ = s.db.Exec(`DELETE FROM coder_session_turn_caps WHERE profile_id = ? AND conversation_id = ? AND turn_id = ?`, profileID, conversationID, turnID)
}

// runningCap is the cap of the turn the session is running now (0 when none, or none capped).
func (s *Store) runningCap(profileID, conversationID string) (turn string, capUSD float64) {
	_ = s.db.QueryRow(`SELECT x.turn_id, COALESCE(k.cap_usd, 0) FROM coder_sessions x
		LEFT JOIN coder_session_turn_caps k ON k.profile_id = x.profile_id AND k.conversation_id = x.conversation_id AND k.turn_id = x.turn_id
		WHERE x.profile_id = ? AND x.conversation_id = ? AND x.turn_id != ''`, profileID, conversationID).Scan(&turn, &capUSD)
	return turn, capUSD
}

// carveReserve is what a carve made now must leave for the session's running turn.
func (s *Store) carveReserve(profileID, conversationID string) float64 {
	_, c := s.runningCap(profileID, conversationID)
	return c
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
	running, _ := s.runningCap(profileID, conversationID)
	total := 0.0
	for turn, c := range caps {
		if v := lead[turn]; v > 0 {
			total += v
		} else if turn != running {
			total += c // unreported (none, or exactly 0): the whole cap; the running turn is charged at its end
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
