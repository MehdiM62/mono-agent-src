// Package coderhost is the persistent side of coder sessions: the state of a
// coder conversation and the commands waiting for it live in the database, and
// the host process (`coder host run`) carries them out, so a session outlives
// the app and the terminal that sent to it. This package holds the store and
// the host loop; starting a turn process is the Launcher's job.
package coderhost

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// State is where a session stands. A paused session starts no new turn; one
// that was paused while a turn ran keeps that turn (TurnPID is set) until it ends.
type State string

const (
	StateIdle        State = "idle"
	StateRunning     State = "running"
	StateWaitingUser State = "waiting_user"
	StatePaused      State = "paused"
	StateStopped     State = "stopped"
)

// Kind is what a command asks the host to do.
type Kind string

const (
	KindSend   Kind = "send"
	KindStop   Kind = "stop"
	KindPause  Kind = "pause"
	KindResume Kind = "resume"
)

// Command statuses.
const (
	CmdPending = "pending"
	CmdDone    = "done"
	CmdFailed  = "failed"
)

// Bounds on what one caller can queue.
const (
	MaxMessageBytes    = 256 << 10
	MaxPendingPerChat  = 50
	MaxAttachments     = 10
	pendingBatch       = 200
	timeLayout         = time.RFC3339Nano
	maxTurnIDLen       = 128
	maxConversationLen = 128
)

// ErrQueueFull is returned by Enqueue when a conversation already has
// MaxPendingPerChat commands waiting.
var ErrQueueFull = errors.New("coder host: too many commands are already waiting for this conversation")

// Command is one request to the host.
type Command struct {
	ID             int64    `json:"id"`
	ProfileID      string   `json:"profileId"`
	ConversationID string   `json:"conversationId"`
	Kind           Kind     `json:"kind"`
	TurnID         string   `json:"turnId,omitempty"`
	Message        string   `json:"message,omitempty"`
	Attachments    []string `json:"attachments,omitempty"`
	Status         string   `json:"status"`
	Error          string   `json:"error,omitempty"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

// Session is a coder conversation as the host tracks it.
type Session struct {
	ProfileID      string `json:"profileId"`
	ConversationID string `json:"conversationId"`
	State          State  `json:"state"`
	Role           string `json:"role"`
	Parent         string `json:"parentConversationId,omitempty"`
	Unattended     bool   `json:"unattended"`
	TurnID         string `json:"turnId,omitempty"`
	TurnPID        int    `json:"turnPid,omitempty"`
	TurnStamp      string `json:"-"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

// Store is the coder_sessions and coder_session_commands tables.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// NewStore wraps an open, migrated database.
func NewStore(db *sql.DB) *Store { return &Store{db: db, now: time.Now} }

func (s *Store) stamp() string { return s.now().UTC().Format(timeLayout) }

// Enqueue validates and queues a command. The host carries it out when the
// session allows it; a send to a busy or paused session waits.
func (s *Store) Enqueue(c Command) (Command, error) {
	switch c.Kind {
	case KindSend:
		if c.Message == "" {
			return c, errors.New("coder host: a send needs a message")
		}
		if len(c.Message) > MaxMessageBytes {
			return c, fmt.Errorf("coder host: the message is %d bytes; the limit is %d", len(c.Message), MaxMessageBytes)
		}
		if c.TurnID == "" || len(c.TurnID) > maxTurnIDLen {
			return c, errors.New("coder host: a send needs a turn id of at most 128 characters")
		}
		if len(c.Attachments) > MaxAttachments {
			return c, fmt.Errorf("coder host: at most %d attachments", MaxAttachments)
		}
	case KindStop, KindPause, KindResume:
	default:
		return c, fmt.Errorf("coder host: unknown command %q", c.Kind)
	}
	if c.ProfileID == "" || c.ConversationID == "" || len(c.ConversationID) > maxConversationLen {
		return c, errors.New("coder host: a command needs a profile and a conversation")
	}
	att, err := json.Marshal(append([]string{}, c.Attachments...))
	if err != nil {
		return c, err
	}
	var waiting int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM coder_session_commands WHERE profile_id = ? AND conversation_id = ? AND status = 'pending'`,
		c.ProfileID, c.ConversationID).Scan(&waiting); err != nil {
		return c, err
	}
	if waiting >= MaxPendingPerChat {
		return c, ErrQueueFull
	}
	now := s.stamp()
	res, err := s.db.Exec(`INSERT INTO coder_session_commands (profile_id, conversation_id, kind, turn_id, message, attachments, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`, c.ProfileID, c.ConversationID, string(c.Kind), c.TurnID, c.Message, string(att), now, now)
	if err != nil {
		return c, err
	}
	c.ID, _ = res.LastInsertId()
	c.Status, c.CreatedAt, c.UpdatedAt = CmdPending, now, now
	return c, nil
}

// Pending returns the commands still waiting, oldest first.
func (s *Store) Pending() ([]Command, error) {
	rows, err := s.db.Query(`SELECT id, profile_id, conversation_id, kind, turn_id, message, attachments, status, error, created_at, updated_at
		FROM coder_session_commands WHERE status = 'pending' ORDER BY id LIMIT ?`, pendingBatch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Command
	for rows.Next() {
		var c Command
		var kind, att string
		if err := rows.Scan(&c.ID, &c.ProfileID, &c.ConversationID, &kind, &c.TurnID, &c.Message, &att, &c.Status, &c.Error, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Kind = Kind(kind)
		_ = json.Unmarshal([]byte(att), &c.Attachments)
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCommand returns one command.
func (s *Store) GetCommand(id int64) (Command, bool, error) {
	rows, err := s.db.Query(`SELECT id, profile_id, conversation_id, kind, turn_id, message, attachments, status, error, created_at, updated_at
		FROM coder_session_commands WHERE id = ?`, id)
	if err != nil {
		return Command{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Command{}, false, rows.Err()
	}
	var c Command
	var kind, att string
	if err := rows.Scan(&c.ID, &c.ProfileID, &c.ConversationID, &kind, &c.TurnID, &c.Message, &att, &c.Status, &c.Error, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return Command{}, false, err
	}
	c.Kind = Kind(kind)
	_ = json.Unmarshal([]byte(att), &c.Attachments)
	return c, true, nil
}

// SetCommand records how a command ended.
func (s *Store) SetCommand(id int64, status, errText string) error {
	_, err := s.db.Exec(`UPDATE coder_session_commands SET status = ?, error = ?, updated_at = ? WHERE id = ?`, status, errText, s.stamp(), id)
	return err
}

const sessionCols = `profile_id, conversation_id, state, role, parent_conversation_id, unattended, turn_id, turn_pid, turn_stamp, created_at, updated_at`

func scanSession(sc interface{ Scan(...any) error }) (Session, error) {
	var x Session
	var state string
	var un int
	err := sc.Scan(&x.ProfileID, &x.ConversationID, &state, &x.Role, &x.Parent, &un, &x.TurnID, &x.TurnPID, &x.TurnStamp, &x.CreatedAt, &x.UpdatedAt)
	x.State, x.Unattended = State(state), un != 0
	return x, err
}

// GetSession returns a session; ok is false when the host has none yet.
func (s *Store) GetSession(profileID, conversationID string) (Session, bool, error) {
	x, err := scanSession(s.db.QueryRow(`SELECT `+sessionCols+` FROM coder_sessions WHERE profile_id = ? AND conversation_id = ?`, profileID, conversationID))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	return x, err == nil, err
}

// EnsureSession returns the session, creating an idle one first.
func (s *Store) EnsureSession(profileID, conversationID string) (Session, error) {
	now := s.stamp()
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO coder_sessions (profile_id, conversation_id, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		profileID, conversationID, now, now); err != nil {
		return Session{}, err
	}
	x, _, err := s.GetSession(profileID, conversationID)
	return x, err
}

// SaveSession writes a session's state and current turn.
func (s *Store) SaveSession(x Session) error {
	_, err := s.db.Exec(`UPDATE coder_sessions SET state = ?, turn_id = ?, turn_pid = ?, turn_stamp = ?, updated_at = ? WHERE profile_id = ? AND conversation_id = ?`,
		string(x.State), x.TurnID, x.TurnPID, x.TurnStamp, s.stamp(), x.ProfileID, x.ConversationID)
	return err
}

// ListSessions returns the sessions of a profile ("" for all), most recently
// changed first.
func (s *Store) ListSessions(profileID string) ([]Session, error) {
	q, args := `SELECT `+sessionCols+` FROM coder_sessions`, []any{}
	if profileID != "" {
		q, args = q+` WHERE profile_id = ?`, append(args, profileID)
	}
	rows, err := s.db.Query(q+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
