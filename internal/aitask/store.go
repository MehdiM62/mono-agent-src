package aitask

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Task statuses.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusNeedsUser = "needs_user"
	StatusComplete  = "complete"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Terminal reports whether a task in status s is over for good.
func Terminal(s string) bool {
	return s == StatusComplete || s == StatusFailed || s == StatusCancelled
}

// Task is Contract A's task JSON.
type Task struct {
	ID             string          `json:"taskId"`
	Kind           string          `json:"kind"`
	ConversationID string          `json:"conversationId"`
	Status         string          `json:"status"`
	Goal           string          `json:"goal"`
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
	ApplicationID  string          `json:"applicationId,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          string          `json:"error,omitempty"`

	// Not in the JSON the callers read.
	Profile    string `json:"-"`
	Access     string `json:"-"`
	Runtime    string `json:"-"`
	Unattended bool   `json:"-"`
	Prompt     string `json:"-"`
}

// Store is the ai_tasks table of one profile.
type Store struct {
	DB      *sql.DB
	Profile string
	Now     func() time.Time
}

func (s *Store) now() string {
	t := time.Now
	if s.Now != nil {
		t = s.Now
	}
	return t().UTC().Format(time.RFC3339Nano)
}

const taskColumns = `id, profile_id, kind, conversation_id, status, goal, application_id, access, runtime,
	unattended, prompt, result_json, error, created_at, updated_at`

type scanner interface{ Scan(...any) error }

func scanTask(r scanner) (Task, error) {
	var t Task
	var unattended int
	var result string
	err := r.Scan(&t.ID, &t.Profile, &t.Kind, &t.ConversationID, &t.Status, &t.Goal, &t.ApplicationID, &t.Access,
		&t.Runtime, &unattended, &t.Prompt, &result, &t.Error, &t.CreatedAt, &t.UpdatedAt)
	t.Unattended = unattended != 0
	if result != "" {
		t.Result = json.RawMessage(result)
	}
	return t, err
}

// NewID is a fresh task id.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "at-" + hex.EncodeToString(b)
}

// Insert stores t for the store's profile.
func (s *Store) Insert(t Task) (Task, error) {
	now := s.now()
	t.Profile, t.CreatedAt, t.UpdatedAt = s.Profile, now, now
	un := 0
	if t.Unattended {
		un = 1
	}
	_, err := s.DB.Exec(`INSERT INTO ai_tasks (id, profile_id, kind, conversation_id, status, goal, application_id,
		access, runtime, unattended, prompt, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Profile, t.Kind, t.ConversationID, t.Status, t.Goal, t.ApplicationID, t.Access, t.Runtime, un,
		t.Prompt, now, now)
	return t, err
}

// Get returns the profile's task id.
func (s *Store) Get(id string) (Task, error) {
	t, err := scanTask(s.DB.QueryRow(`SELECT `+taskColumns+` FROM ai_tasks WHERE id = ? AND profile_id = ?`, id, s.Profile))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, errf(CodeNotFound, "no AI task %s", id)
	}
	return t, err
}

// List returns the profile's tasks, newest first, filtered by kind and
// application when those are set.
func (s *Store) List(kind, applicationID string) ([]Task, error) {
	q := `SELECT ` + taskColumns + ` FROM ai_tasks WHERE profile_id = ?`
	args := []any{s.Profile}
	if kind != "" {
		q += ` AND kind = ?`
		args = append(args, kind)
	}
	if applicationID != "" {
		q += ` AND application_id = ?`
		args = append(args, applicationID)
	}
	rows, err := s.DB.Query(q+` ORDER BY created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// move changes a task's status in one statement that holds only while the
// task is in one of from, so two callers never both win. extra are more
// columns to set ("error = ?" ...) with their values.
func (s *Store) move(id string, from []string, to string, extra map[string]any) (bool, error) {
	set, args := []string{"status = ?", "updated_at = ?"}, []any{to, s.now()}
	for _, col := range []string{"conversation_id", "error", "result_json", "token_hash"} {
		if v, ok := extra[col]; ok {
			set, args = append(set, col+" = ?"), append(args, v)
		}
	}
	args = append(args, id, s.Profile)
	in := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
	for _, f := range from {
		args = append(args, f)
	}
	res, err := s.DB.Exec(`UPDATE ai_tasks SET `+strings.Join(set, ", ")+
		` WHERE id = ? AND profile_id = ? AND status IN (`+in+`)`, args...)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// closedError is the refusal of a change to a task that is no longer in the
// state the change needs.
func closedError(t Task, what string) *Error {
	return &Error{Code: CodeTaskClosed, Message: "cannot " + what + ": task " + t.ID + " is " + t.Status,
		Fields: map[string]any{"status": t.Status}}
}

// NewToken is a fresh task token: the secret the task's turn runs with.
func NewToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// HashToken is what is stored of a token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// SetToken stores the hash of the token the task's next turn runs with.
func (s *Store) SetToken(id, token string) error {
	_, err := s.DB.Exec(`UPDATE ai_tasks SET token_hash = ? WHERE id = ? AND profile_id = ?`, HashToken(token), id, s.Profile)
	return err
}

// CheckToken says whether token is the one stored for task id, in constant
// time. A task with no token (never started) accepts none.
func (s *Store) CheckToken(id, token string) bool {
	var stored string
	if err := s.DB.QueryRow(`SELECT token_hash FROM ai_tasks WHERE id = ? AND profile_id = ?`, id, s.Profile).Scan(&stored); err != nil {
		return false
	}
	if stored == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(HashToken(token))) == 1
}
