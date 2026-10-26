package coderhost

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// A mission confirmation is the operator's explicit, per-turn "yes, run this as
// a mission". It is a record the operator-gated `coder sessions send --mission`
// writes (in the configs table, beside the coder settings that carry the other
// operator-only acknowledgement, `coder enable`); the command row, the message,
// the attachments and the mesh block carry nothing of it. The host reads it when
// it starts the turn and hands the child its nonce; the child checks the record
// again and consumes it, so one confirmation starts one turn, once.

// MissionConfirmTTL is how long a confirmation lives before its turn starts.
const MissionConfirmTTL = 10 * time.Minute

const confirmPrefix = "coder-mission-confirm/"

// MissionConfirm is what the operator confirmed: this turn of this conversation,
// for the mission state it was given in (Session is the open mission session of
// the conversation then, 0 when there was none and the lead was only proposing).
type MissionConfirm struct {
	ProfileID      string `json:"profile"`
	ConversationID string `json:"conversation"`
	TurnID         string `json:"turn"`
	Session        int64  `json:"session"`
	Nonce          string `json:"nonce"`
	Expires        string `json:"expires"`
}

// HostMade says whether a turn id is one the host makes on its own (a
// check-in), not a person's send. Such a turn can never be confirmed.
func HostMade(turnID string) bool { return strings.HasPrefix(turnID, "checkin-") }

var errHostMade = errors.New("coder host: a host-started turn cannot carry a mission confirmation")

func confirmName(profile, conv, turn string) string {
	return confirmPrefix + profile + "/" + conv + "/" + turn
}

// Confirm records the operator's confirmation for one turn. Only the
// operator-gated send command calls it.
func (s *Store) Confirm(profileID, conversationID, turnID string, session int64) (MissionConfirm, error) {
	if HostMade(turnID) {
		return MissionConfirm{}, errHostMade
	}
	if profileID == "" || conversationID == "" || turnID == "" {
		return MissionConfirm{}, errors.New("coder host: a confirmation names a profile, conversation and turn")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return MissionConfirm{}, err
	}
	c := MissionConfirm{ProfileID: profileID, ConversationID: conversationID, TurnID: turnID, Session: session,
		Nonce: hex.EncodeToString(nonce), Expires: s.now().Add(MissionConfirmTTL).UTC().Format(timeLayout)}
	b, _ := json.Marshal(c)
	// Expired confirmations of any turn are swept as a new one is written.
	s.sweepConfirms()
	_, err := s.db.Exec(`INSERT INTO configs (name, config_data, created_at, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET config_data = excluded.config_data, updated_at = excluded.updated_at`,
		confirmName(profileID, conversationID, turnID), string(b))
	return c, err
}

func (c MissionConfirm) live(now time.Time) bool {
	t, err := time.Parse(timeLayout, c.Expires)
	return err == nil && now.Before(t)
}

func (s *Store) sweepConfirms() {
	rows, err := s.db.Query(`SELECT name, config_data FROM configs WHERE name LIKE ?`, confirmPrefix+"%")
	if err != nil {
		return
	}
	var dead []string
	for rows.Next() {
		var name, raw string
		var c MissionConfirm
		if rows.Scan(&name, &raw) == nil && (json.Unmarshal([]byte(raw), &c) != nil || !c.live(s.now())) {
			dead = append(dead, name)
		}
	}
	rows.Close()
	for _, n := range dead {
		_, _ = s.db.Exec(`DELETE FROM configs WHERE name = ?`, n)
	}
}

// Revoke drops an unconsumed confirmation (a send that could not be queued).
func (s *Store) Revoke(profileID, conversationID, turnID string) {
	_, _ = s.db.Exec(`DELETE FROM configs WHERE name = ?`, confirmName(profileID, conversationID, turnID))
}

func (s *Store) readConfirm(profileID, conversationID, turnID string) (MissionConfirm, string, bool) {
	var raw string
	if err := s.db.QueryRow(`SELECT config_data FROM configs WHERE name = ?`, confirmName(profileID, conversationID, turnID)).Scan(&raw); err != nil {
		return MissionConfirm{}, "", false // no record (or unreadable): no confirmation
	}
	var c MissionConfirm
	if json.Unmarshal([]byte(raw), &c) != nil || c.ProfileID != profileID || c.ConversationID != conversationID || c.TurnID != turnID || c.Nonce == "" {
		return MissionConfirm{}, "", false
	}
	if !c.live(s.now()) {
		return MissionConfirm{}, "", false
	}
	return c, raw, true
}

// PeekConfirm is what the host looks at when it starts a turn: the live
// confirmation of this turn, never one for a host-made turn.
func (s *Store) PeekConfirm(profileID, conversationID, turnID string) (MissionConfirm, bool) {
	if HostMade(turnID) {
		return MissionConfirm{}, false
	}
	c, _, ok := s.readConfirm(profileID, conversationID, turnID)
	return c, ok
}

// ConsumeConfirm is the chat child's check: the nonce and the mission state
// (session) must be those the operator confirmed, and the record is deleted in
// the same statement that proves it still holds, so a second start, a restart
// or a concurrent child gets false.
func (s *Store) ConsumeConfirm(profileID, conversationID, turnID, nonce string, session int64) bool {
	if HostMade(turnID) || nonce == "" {
		return false
	}
	c, raw, ok := s.readConfirm(profileID, conversationID, turnID)
	if !ok || c.Nonce != nonce || c.Session != session {
		return false
	}
	res, err := s.db.Exec(`DELETE FROM configs WHERE name = ? AND config_data = ?`, confirmName(profileID, conversationID, turnID), raw)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}
