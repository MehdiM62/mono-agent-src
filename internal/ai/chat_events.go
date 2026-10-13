package ai

import (
	"errors"
	"fmt"
	"time"
)

// ErrTurnActive is returned by DeleteConversation when a turn is still
// active — deletion while a supervisor may be mid-write would race the
// journal and could leave a running process pointed at a vanished
// conversation.
var ErrTurnActive = errors.New("chat: conversation has an active turn")

// ErrConversationNotFound / ErrTurnNotFound distinguish "doesn't exist or
// isn't yours" from a generic sql.ErrNoRows leaking scoping details to
// callers — every lookup here is profile-scoped, so a wrong profileID and a
// truly missing ID look identical on purpose.
var (
	ErrConversationNotFound = errors.New("chat: conversation not found")
	ErrTurnNotFound         = errors.New("chat: turn not found")
)

// ErrTurnOwnedByOtherInstance is returned by CreateTurn when the target
// conversation already has an active turn owned by a DIFFERENT, identified
// live app instance. Two GUI processes sharing one ~/.monoagent database
// must not each admit and run a turn against the same conversation —
// whichever finished last would otherwise silently overwrite the other's
// --resume session binding via BindConversationSession. See
// docs/mastermind/plans/2026-09-12-interactive-agent-chat-followups.md,
// "OwnerInstanceID is written and read back but never compared to anything".
var ErrTurnOwnedByOtherInstance = errors.New("chat: conversation has an active turn owned by another instance")

// Conversation is one server-created app-level chat conversation — see the
// plan's "three distinct identifiers" section. HistoryKey is the opaque
// provider model-context bucket; it is deliberately excluded from JSON so
// it never reaches the frontend or a Wails response.
type Conversation struct {
	ID              string `json:"id"`
	ProfileID       string `json:"profileId"`
	Backend         string `json:"backend"` // "agent", or "provider" for a read-only legacy conversation
	WorkflowContext string `json:"workflowContext"`
	RuntimeID       string `json:"runtimeId,omitempty"`
	ProviderID      string `json:"providerId,omitempty"`
	Model           string `json:"model,omitempty"`
	Effort          string `json:"effort,omitempty"`
	SessionID       string `json:"sessionId,omitempty"`
	// Mode is "assistant" or "coder". A coder conversation runs every turn
	// with full access inside Cwd, fixed at creation: Claude Code keys its
	// sessions by folder, so resuming needs the same one.
	Mode string `json:"mode"`
	Cwd  string `json:"cwd"`
	// OrgMode is a coder conversation's org: "solo" (the agent works
	// alone) or "dynamic" (it can spawn workers, #226).
	OrgMode    string `json:"orgMode,omitempty"`
	HistoryKey string `json:"-"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

// Turn is one client-registered turn within a conversation — the mutable
// projection the supervisor updates as a run progresses and finalizes.
type Turn struct {
	ID               string `json:"id"`
	ConversationID   string `json:"conversationId"`
	ProfileID        string `json:"profileId"`
	OwnerInstanceID  string `json:"-"`
	Prompt           string `json:"prompt"`
	Status           string `json:"status"` // active|completed|failed|cancelled|interrupted
	Reason           string `json:"reason,omitempty"`
	LastCommittedSeq int64  `json:"lastCommittedSeq"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

const turnStatusActive = "active"

func (s *AIStore) initChatEventTables() error {
	if err := s.ensureAnswersTable(); err != nil {
		return err
	}
	const conversationsSQL = `CREATE TABLE IF NOT EXISTS ai_chat_conversations (
		id TEXT PRIMARY KEY,
		profile_id TEXT NOT NULL,
		backend TEXT NOT NULL,
		workflow_context TEXT NOT NULL DEFAULT '',
		runtime_id TEXT NOT NULL DEFAULT '',
		provider_id TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		session_id TEXT NOT NULL DEFAULT '',
		history_key TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`
	// conversation_id carries a real foreign key back to
	// ai_chat_conversations(id) ON DELETE CASCADE (added, for an existing
	// database, by data/migrations/040_ai_chat_conversation_foreign_keys.sql
	// via the standard SQLite rebuild recipe — SQLite cannot ALTER TABLE ADD
	// FOREIGN KEY). It is declared here too so a caller that constructs
	// AIStore directly against a database that never ran ApplyMigrations
	// still gets the constraint on a genuinely fresh CREATE TABLE; on any
	// database that DID run migration 040 first (every real CLI/GUI startup
	// path), this CREATE TABLE IF NOT EXISTS is a no-op and the migration's
	// shape is what's actually in effect. See
	// docs/mastermind/plans/2026-09-12-interactive-agent-chat-followups.md,
	// "DeleteConversation isn't atomic against a concurrent StartChatTurn".
	const turnsSQL = `CREATE TABLE IF NOT EXISTS ai_chat_turns (
		id TEXT PRIMARY KEY,
		conversation_id TEXT NOT NULL,
		profile_id TEXT NOT NULL,
		owner_instance_id TEXT NOT NULL DEFAULT '',
		prompt TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		reason TEXT NOT NULL DEFAULT '',
		last_committed_seq INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY (conversation_id) REFERENCES ai_chat_conversations(id) ON DELETE CASCADE
	)`
	// Composite primary key doubles as the unique (profile_id,
	// conversation_id, turn_id, seq) constraint the plan requires, and gives
	// an efficient natural index for "events after seq N" catch-up queries.
	// conversation_id and turn_id each carry a real foreign key (see turnsSQL
	// above for why they are declared here as well as in migration 040) —
	// turn_id's target, ai_chat_turns, is safe to require because every
	// AppendEvent call in this codebase (cmd/monoagentcli's turnJournal, the
	// only production writer) is already structurally
	// downstream of a successful CreateTurn for that same turn ID.
	const eventsSQL = `CREATE TABLE IF NOT EXISTS ai_chat_events (
		profile_id TEXT NOT NULL,
		conversation_id TEXT NOT NULL,
		turn_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		version INTEGER NOT NULL,
		type TEXT NOT NULL,
		payload TEXT NOT NULL,
		PRIMARY KEY (profile_id, conversation_id, turn_id, seq),
		FOREIGN KEY (conversation_id) REFERENCES ai_chat_conversations(id) ON DELETE CASCADE,
		FOREIGN KEY (turn_id) REFERENCES ai_chat_turns(id) ON DELETE CASCADE
	)`
	if _, err := s.db.Exec(conversationsSQL); err != nil {
		return fmt.Errorf("create ai_chat_conversations: %w", err)
	}
	if _, err := s.db.Exec(turnsSQL); err != nil {
		return fmt.Errorf("create ai_chat_turns: %w", err)
	}
	if _, err := s.db.Exec(eventsSQL); err != nil {
		return fmt.Errorf("create ai_chat_events: %w", err)
	}
	for _, alter := range []string{
		`ALTER TABLE ai_chat_conversations ADD COLUMN mode TEXT NOT NULL DEFAULT 'assistant'`,
		`ALTER TABLE ai_chat_conversations ADD COLUMN cwd TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_chat_conversations ADD COLUMN effort TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE ai_chat_conversations ADD COLUMN org_mode TEXT NOT NULL DEFAULT 'solo'`,
	} {
		if err := addColumnIfMissing(s.db, alter); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_chat_conversations_profile ON ai_chat_conversations(profile_id, updated_at DESC)`); err != nil {
		return fmt.Errorf("create idx_ai_chat_conversations_profile: %w", err)
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_ai_chat_turns_conversation ON ai_chat_turns(conversation_id, profile_id, created_at DESC)`); err != nil {
		return fmt.Errorf("create idx_ai_chat_turns_conversation: %w", err)
	}
	return nil
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// ConversationRecord is a Conversation as `monoagentcli chat history`
// prints it: snake_case keys, and no history key.
type ConversationRecord struct {
	ID              string `json:"id"`
	ProfileID       string `json:"profile_id"`
	Backend         string `json:"backend"`
	WorkflowContext string `json:"workflow_context"`
	RuntimeID       string `json:"runtime_id"`
	ProviderID      string `json:"provider_id"`
	Model           string `json:"model"`
	Effort          string `json:"effort,omitempty"`
	SessionID       string `json:"session_id"`
	Mode            string `json:"mode"`
	Cwd             string `json:"cwd"`
	OrgMode         string `json:"org_mode,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// Record converts c to its CLI shape.
func (c Conversation) Record() ConversationRecord {
	return ConversationRecord{
		ID: c.ID, ProfileID: c.ProfileID, Backend: c.Backend, WorkflowContext: c.WorkflowContext,
		RuntimeID: c.RuntimeID, ProviderID: c.ProviderID, Model: c.Model, Effort: c.Effort, SessionID: c.SessionID,
		Mode: c.Mode, Cwd: c.Cwd, OrgMode: c.OrgMode, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// Conversation converts a CLI record back (HistoryKey stays empty).
func (r ConversationRecord) Conversation() Conversation {
	return Conversation{
		ID: r.ID, ProfileID: r.ProfileID, Backend: r.Backend, WorkflowContext: r.WorkflowContext,
		RuntimeID: r.RuntimeID, ProviderID: r.ProviderID, Model: r.Model, Effort: r.Effort, SessionID: r.SessionID,
		Mode: r.Mode, Cwd: r.Cwd, OrgMode: r.OrgMode, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// TurnRecord is a Turn as the CLI prints it. Unlike Turn's own JSON it
// carries the owner instance id, so the app can tell its own active turns
// from another window's.
type TurnRecord struct {
	ID               string `json:"id"`
	ConversationID   string `json:"conversation_id"`
	ProfileID        string `json:"profile_id"`
	OwnerInstanceID  string `json:"owner_instance_id"`
	Prompt           string `json:"prompt"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	LastCommittedSeq int64  `json:"last_committed_seq"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// Record converts t to its CLI shape.
func (t Turn) Record() TurnRecord {
	return TurnRecord{
		ID: t.ID, ConversationID: t.ConversationID, ProfileID: t.ProfileID, OwnerInstanceID: t.OwnerInstanceID,
		Prompt: t.Prompt, Status: t.Status, Reason: t.Reason, LastCommittedSeq: t.LastCommittedSeq,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

// Turn converts a CLI record back.
func (r TurnRecord) Turn() Turn {
	return Turn{
		ID: r.ID, ConversationID: r.ConversationID, ProfileID: r.ProfileID, OwnerInstanceID: r.OwnerInstanceID,
		Prompt: r.Prompt, Status: r.Status, Reason: r.Reason, LastCommittedSeq: r.LastCommittedSeq,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}
