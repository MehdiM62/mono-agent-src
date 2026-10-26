package coderhost

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Parked questions: a gap that nobody present can settle is written down, the
// session goes to waiting_user, and the other sessions carry on. The question is
// answered by the one the policy names (Decide), and the answer reaches the
// session as a queued send whose turn id starts with AnswerPrefix: like a
// check-in it is host-made, counts toward the check-in limit and can never carry
// a mission confirmation (HostMade).

// AnswerPrefix starts the turn id of a send the host queues to hand an answer
// (or an applied default) to a session.
const AnswerPrefix = "answer-"

// Bounds.
const (
	// ParkTimeout is how long a question waits for its answerer before its
	// timeout fallback applies.
	ParkTimeout           = 30 * time.Minute
	MaxOpenQuestions      = 20
	maxQuestionText       = 2000
	maxOptions            = 10
	maxOptionText         = 500
	MaxAnswerBytes        = 8 << 10
	questionStatusOpen    = "open"
	questionStatusAnswer  = "answered"
	questionStatusDefault = "defaulted"
)

// CallerScope says who is answering. TODO(P4): the master's side is the
// descendant-token mechanism of phase P4, which does not exist on this branch;
// when it lands, its verified token should produce a CallerScope with Role()
// "master" and ConversationID() the master's own conversation. Until then only
// OperatorScope exists, so nothing can answer as a master.
type CallerScope interface {
	Role() string           // WhoUser or WhoMaster
	ConversationID() string // the master's own conversation; "" for the operator
}

// OperatorScope is the person at the operator CLI.
type OperatorScope struct{}

// Role is WhoUser.
func (OperatorScope) Role() string { return WhoUser }

// ConversationID is empty: the operator is no conversation.
func (OperatorScope) ConversationID() string { return "" }

// Question is a parked question.
type Question struct {
	ID             int64    `json:"id"`
	ProfileID      string   `json:"profileId"`
	ConversationID string   `json:"conversationId"`
	Class          string   `json:"class"`
	Question       string   `json:"question"`
	Options        []string `json:"options"`
	Default        string   `json:"default,omitempty"`
	Reversible     bool     `json:"reversible"`
	Status         string   `json:"status"` // open | answered | defaulted
	Answer         string   `json:"answer,omitempty"`
	AnsweredBy     string   `json:"answeredBy,omitempty"`
	CommandID      int64    `json:"commandId,omitempty"`
	CreatedAt      string   `json:"createdAt"`
	AnsweredAt     string   `json:"answeredAt,omitempty"`
	// Answerer is who the policy names now; it is filled by ListQuestions and Park, not stored.
	Answerer string `json:"answerer,omitempty"`
}

// Errors.
var (
	// ErrModelDecides: the policy lets the model settle this gap, so there is
	// nothing to park; the caller decides and writes it to the ledger.
	ErrModelDecides = errors.New("coder host: the policy lets the model decide this gap; nothing to park")
	ErrTooManyOpen  = errors.New("coder host: too many questions are already parked for this conversation")
	ErrNoQuestion   = errors.New("coder host: no such question")
	ErrNotOpen      = errors.New("coder host: this question was already answered")
	ErrNotAnswerer  = errors.New("coder host: the policy names someone else to answer this question")
)

const questionCols = `id, profile_id, conversation_id, class, question, options, default_answer, reversible, status, answer, answered_by, command_id, created_at, answered_at`

func scanQuestion(sc interface{ Scan(...any) error }) (Question, error) {
	var q Question
	var opts string
	var rev int
	err := sc.Scan(&q.ID, &q.ProfileID, &q.ConversationID, &q.Class, &q.Question, &opts, &q.Default, &rev, &q.Status, &q.Answer, &q.AnsweredBy, &q.CommandID, &q.CreatedAt, &q.AnsweredAt)
	q.Reversible = rev != 0
	q.Options = []string{}
	_ = json.Unmarshal([]byte(opts), &q.Options)
	return q, err
}

// hasMaster says whether the session is a child with a parent to ask.
func (s *Store) hasMaster(profileID, conversationID string) (parent string, ok bool) {
	x, found, err := s.GetSession(profileID, conversationID)
	if err != nil || !found || x.Role != "child" || x.Parent == "" {
		return "", false
	}
	return x.Parent, true
}

// answererNow is who may answer a parked question under the settings as they are
// now. A question already parked never goes to the model by way of an answer:
// when the policy now says model, a master answers it (or the user, without one).
func (s *Store) answererNow(q Question) (string, error) {
	settings, err := s.GetSettings(q.ProfileID, q.ConversationID)
	if err != nil {
		return "", err
	}
	_, has := s.hasMaster(q.ProfileID, q.ConversationID)
	who := Decide(settings, q.Class, q.Reversible, has).Answerer
	if who == WhoModel {
		who = WhoMaster
		if !has {
			who = WhoUser
		}
	}
	return who, nil
}

// Park writes a question nobody present can settle and puts the session into
// waiting_user (an idle session; a running one gets there when its turn ends,
// a paused or stopped one keeps its state). It returns ErrModelDecides when
// the policy lets the model settle this class, since then nobody needs to be
// asked. def is what the host applies after ParkTimeout, only where the
// policy's fallback is take_default.
func (s *Store) Park(profileID, conversationID, class, question string, options []string, def string, reversible bool) (Question, error) {
	if profileID == "" || conversationID == "" || len(conversationID) > maxConversationLen {
		return Question{}, errors.New("coder host: a question needs a profile and a conversation")
	}
	if !oneOf(class, GapClasses) {
		return Question{}, fmt.Errorf("coder host: %q is not a class of gap", class)
	}
	if strings.TrimSpace(question) == "" {
		return Question{}, errors.New("coder host: a question needs a text")
	}
	settings, err := s.GetSettings(profileID, conversationID)
	if err != nil {
		return Question{}, err
	}
	_, has := s.hasMaster(profileID, conversationID)
	if Decide(settings, class, reversible, has).Answerer == WhoModel {
		return Question{}, ErrModelDecides
	}
	if s.openCount(profileID, conversationID) >= MaxOpenQuestions {
		return Question{}, ErrTooManyOpen
	}
	opts := []string{}
	for _, o := range options {
		if len(opts) == maxOptions {
			break
		}
		opts = append(opts, trimTo(o, maxOptionText))
	}
	raw, _ := json.Marshal(opts)
	if _, err := s.EnsureSession(profileID, conversationID); err != nil {
		return Question{}, err
	}
	rev := 0
	if reversible {
		rev = 1
	}
	now := s.stamp()
	res, err := s.db.Exec(`INSERT INTO coder_session_questions (profile_id, conversation_id, class, question, options, default_answer, reversible, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'open', ?)`, profileID, conversationID, class, trimTo(question, maxQuestionText), string(raw), trimTo(def, maxQuestionText), rev, now)
	if err != nil {
		return Question{}, err
	}
	id, _ := res.LastInsertId()
	// Only an idle session moves: a running turn settles into waiting_user itself, and
	// pause and stop keep their meaning.
	if _, err := s.db.Exec(`UPDATE coder_sessions SET state = 'waiting_user', updated_at = ? WHERE profile_id = ? AND conversation_id = ? AND state = 'idle'`, now, profileID, conversationID); err != nil {
		return Question{}, err
	}
	return s.GetQuestion(profileID, id)
}

func (s *Store) openCount(profileID, conversationID string) int {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM coder_session_questions WHERE profile_id = ? AND conversation_id = ? AND status = 'open'`, profileID, conversationID).Scan(&n); err != nil {
		return 1 << 30
	}
	return n
}

// OpenParked counts a conversation's questions still waiting for an answer.
func (s *Store) OpenParked(profileID, conversationID string) int {
	return s.openCount(profileID, conversationID)
}

// GetQuestion returns one question of a profile.
func (s *Store) GetQuestion(profileID string, id int64) (Question, error) {
	q, err := scanQuestion(s.db.QueryRow(`SELECT `+questionCols+` FROM coder_session_questions WHERE profile_id = ? AND id = ?`, profileID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Question{}, ErrNoQuestion
	}
	if err != nil {
		return Question{}, err
	}
	q.Answerer, _ = s.answererNow(q)
	return q, nil
}

// ListQuestions returns a profile's questions, oldest first; conversationID ""
// means every conversation, and onlyOpen leaves out the answered ones.
func (s *Store) ListQuestions(profileID, conversationID string, onlyOpen bool) ([]Question, error) {
	q, args := `SELECT `+questionCols+` FROM coder_session_questions WHERE profile_id = ?`, []any{profileID}
	if conversationID != "" {
		q, args = q+` AND conversation_id = ?`, append(args, conversationID)
	}
	if onlyOpen {
		q += ` AND status = 'open'`
	}
	rows, err := s.db.Query(q+` ORDER BY id LIMIT ?`, append(args, listLimitMax)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Question{}
	for rows.Next() {
		x, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Status == questionStatusOpen {
			out[i].Answerer, _ = s.answererNow(out[i])
		}
	}
	return out, nil
}

func answerTurnID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return AnswerPrefix + hex.EncodeToString(b)
}

// answerText is the message the session receives.
func answerText(q Question, by, answer string, defaulted bool) string {
	var b strings.Builder
	if defaulted {
		fmt.Fprintf(&b, "[parked question #%d (%s): nobody answered in time, so the default was applied]\n", q.ID, q.Class)
	} else {
		fmt.Fprintf(&b, "[answer to your parked question #%d (%s), given by the %s]\n", q.ID, q.Class, by)
	}
	fmt.Fprintf(&b, "Question: %s\n", q.Question)
	if defaulted {
		fmt.Fprintf(&b, "Default applied: %s\n", answer)
	} else {
		fmt.Fprintf(&b, "Answer: %s\n", answer)
	}
	return b.String()
}

// Answer settles an open question. It is allowed only for the answerer the
// policy names now (and for a master, only the session's own master); the
// answer is queued to the session as a send with an AnswerPrefix turn id. It
// carries no authority beyond text: it cannot change access, a budget, a
// setting, or pass a send gate, and it can never be a mission confirmation.
func (s *Store) Answer(profileID string, id int64, answer string, scope CallerScope) (Question, Command, error) {
	if scope == nil {
		return Question{}, Command{}, ErrNotAnswerer
	}
	if strings.TrimSpace(answer) == "" || len(answer) > MaxAnswerBytes {
		return Question{}, Command{}, fmt.Errorf("coder host: an answer is 1 to %d bytes", MaxAnswerBytes)
	}
	q, err := s.GetQuestion(profileID, id)
	if err != nil {
		return q, Command{}, err
	}
	if q.Status != questionStatusOpen {
		return q, Command{}, ErrNotOpen
	}
	who, err := s.answererNow(q)
	if err != nil {
		return q, Command{}, err
	}
	if scope.Role() != who {
		return q, Command{}, fmt.Errorf("%w (the policy names the %s)", ErrNotAnswerer, who)
	}
	if who == WhoMaster {
		if parent, ok := s.hasMaster(q.ProfileID, q.ConversationID); !ok || scope.ConversationID() != parent {
			return q, Command{}, ErrNotAnswerer
		}
	}
	by := scope.Role()
	if err := s.claimQuestion(q, questionStatusAnswer, answer, by); err != nil {
		return q, Command{}, err
	}
	cmd, err := s.Enqueue(Command{ProfileID: q.ProfileID, ConversationID: q.ConversationID, Kind: KindSend, TurnID: answerTurnID(), Message: answerText(q, by, answer, false)})
	if err != nil {
		s.reopenQuestion(q.ID) // not delivered: the question stays open
		return q, Command{}, err
	}
	_, _ = s.db.Exec(`UPDATE coder_session_questions SET command_id = ? WHERE id = ?`, cmd.ID, q.ID)
	s.releaseWaiting(q.ProfileID, q.ConversationID)
	q.Status, q.Answer, q.AnsweredBy, q.CommandID = questionStatusAnswer, answer, by, cmd.ID
	return q, cmd, nil
}

// claimQuestion closes an open question atomically, so two answers (or an answer
// and a default) cannot both win.
func (s *Store) claimQuestion(q Question, status, answer, by string) error {
	res, err := s.db.Exec(`UPDATE coder_session_questions SET status = ?, answer = ?, answered_by = ?, answered_at = ? WHERE id = ? AND status = 'open'`,
		status, trimTo(answer, MaxAnswerBytes), by, s.stamp(), q.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotOpen
	}
	return nil
}

func (s *Store) reopenQuestion(id int64) {
	_, _ = s.db.Exec(`UPDATE coder_session_questions SET status = 'open', answer = '', answered_by = '', answered_at = '' WHERE id = ?`, id)
}

// releaseWaiting returns a waiting_user session to idle once no question is open.
func (s *Store) releaseWaiting(profileID, conversationID string) {
	if s.openCount(profileID, conversationID) > 0 {
		return
	}
	_, _ = s.db.Exec(`UPDATE coder_sessions SET state = 'idle', updated_at = ? WHERE profile_id = ? AND conversation_id = ? AND state = 'waiting_user'`, s.stamp(), profileID, conversationID)
}

// ApplyDefaults applies the timeout fallback to questions that waited longer
// than ParkTimeout: where the policy's fallback is take_default (never for a
// hard class: Decide clamps it to park) and the question has a default, the host
// records a model decision in the ledger and tells the session. Everything else
// stays parked. It returns how many defaults it applied.
func (s *Store) ApplyDefaults() (int, error) {
	cut := s.now().Add(-ParkTimeout).UTC().Format(timeLayout)
	rows, err := s.db.Query(`SELECT `+questionCols+` FROM coder_session_questions WHERE status = 'open' AND default_answer != '' AND created_at < ? ORDER BY id LIMIT ?`, cut, pendingBatch)
	if err != nil {
		return 0, err
	}
	var due []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, q)
	}
	rows.Close()
	applied := 0
	var errs []error
	for _, q := range due {
		settings, err := s.GetSettings(q.ProfileID, q.ConversationID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		_, has := s.hasMaster(q.ProfileID, q.ConversationID)
		if Decide(settings, q.Class, q.Reversible, has).Fallback != FallbackTakeDefault {
			continue
		}
		if err := s.claimQuestion(q, questionStatusDefault, q.Default, WhoModel); err != nil {
			continue // answered meanwhile
		}
		if _, err := s.Record(LedgerEntry{ProfileID: q.ProfileID, ConversationID: q.ConversationID, Class: q.Class, Question: q.Question, Choice: q.Default,
			Alternatives: q.Options, Reason: "nobody answered within " + ParkTimeout.String() + "; the stated default was applied", Reversible: q.Reversible, MadeBy: WhoModel}); err != nil {
			s.reopenQuestion(q.ID)
			errs = append(errs, err)
			continue
		}
		cmd, err := s.Enqueue(Command{ProfileID: q.ProfileID, ConversationID: q.ConversationID, Kind: KindSend, TurnID: answerTurnID(), Message: answerText(q, WhoModel, q.Default, true)})
		if err != nil {
			errs = append(errs, err) // the ledger row and the closed question stand; the note could not be queued
		} else {
			_, _ = s.db.Exec(`UPDATE coder_session_questions SET command_id = ? WHERE id = ?`, cmd.ID, q.ID)
		}
		s.releaseWaiting(q.ProfileID, q.ConversationID)
		applied++
	}
	return applied, errors.Join(errs...)
}

// parkedPass is the host's periodic look at parked questions.
func (h *Host) parkedPass() {
	if _, err := h.Store.ApplyDefaults(); err != nil {
		h.logf("coder host: parked questions: %v", err)
	}
}
