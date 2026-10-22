package ai

import (
	"fmt"
	"slices"
)

// Access levels of a conversation. They replace the two modes as the thing a
// turn is chosen by: Mode stays on the row (assistant for scoped, coder for
// the rest) for older readers, and is always derived from the level.
//
//	scoped    the sandboxed assistant: mono-agent's own tools, canvas, page read
//	coding    a coding-agent session in a folder, told nothing about the profile
//	workspace the same, plus the profile's workspace summary (coder.appAccess)
//	full      the same as workspace; the level a coder conversation always had
//
// The order is the order of power: only the operator may raise a level.
const (
	AccessScoped    = "scoped"
	AccessCoding    = "coding"
	AccessWorkspace = "workspace"
	AccessFull      = "full"
)

var accessLevels = []string{AccessScoped, AccessCoding, AccessWorkspace, AccessFull}

// AccessLevels lists the levels, weakest first.
func AccessLevels() []string { return slices.Clone(accessLevels) }

// ValidAccess reports whether a is a level.
func ValidAccess(a string) bool { return slices.Contains(accessLevels, a) }

// AccessRank orders the levels (scoped 0 … full 3); an unknown one is -1.
func AccessRank(a string) int { return slices.Index(accessLevels, a) }

// AccessForMode is the level an old --mode stands for: assistant is scoped,
// coder is full (what a coder conversation always had).
func AccessForMode(mode string) string {
	if mode == ModeCoder {
		return AccessFull
	}
	return AccessScoped
}

// ModeForAccess is the mode kept on the row for a level.
func ModeForAccess(access string) string {
	if access == AccessScoped {
		return ModeAssistant
	}
	return ModeCoder
}

// EffectiveAccess is the conversation's level; a value built without one
// (an older record) takes it from its mode.
func (c Conversation) EffectiveAccess() string {
	if ValidAccess(c.Access) {
		return c.Access
	}
	return AccessForMode(c.Mode)
}

// Scoped reports whether the conversation runs as the sandboxed assistant.
func (c Conversation) Scoped() bool { return c.EffectiveAccess() == AccessScoped }

// SetConversationAccess moves a conversation to a level and returns the
// updated row. It does not decide who may: the CLI does (raising is the
// operator's). A level other than scoped needs a folder, taken from cwd or
// the one the row already has. Mode follows the level, a conversation that
// leaves or enters scoped starts a fresh session (a runtime's sessions are
// keyed by its folder, and a sandboxed one is not a coding one), and going
// down to scoped puts the org back to solo.
func (s *AIStore) SetConversationAccess(id, profileID, access, cwd string) (Conversation, error) {
	if profileID == "" {
		return Conversation{}, ErrProfileRequired
	}
	if !ValidAccess(access) {
		return Conversation{}, fmt.Errorf("set access: unknown access %q", access)
	}
	conv, err := s.GetConversation(id, profileID)
	if err != nil {
		return Conversation{}, err
	}
	if cwd == "" {
		cwd = conv.Cwd
	}
	if access != AccessScoped && cwd == "" {
		return Conversation{}, fmt.Errorf("set access: access %q needs a folder", access)
	}
	session, org := conv.SessionID, conv.OrgMode
	if (access == AccessScoped) != conv.Scoped() {
		session = ""
	}
	if access == AccessScoped {
		org = OrgModeSolo
	}
	if _, err := s.db.Exec(`UPDATE ai_chat_conversations SET access = ?, mode = ?, cwd = ?, session_id = ?, org_mode = ?, updated_at = ?
		WHERE id = ? AND profile_id = ?`, access, ModeForAccess(access), cwd, session, org, nowRFC3339(), id, profileID); err != nil {
		return Conversation{}, fmt.Errorf("set access: %w", err)
	}
	return s.GetConversation(id, profileID)
}

// ForgetOrgWorkers drops a conversation's kept dynamic-org workers, so a
// later turn cannot resume them.
func (s *AIStore) ForgetOrgWorkers(profileID, conversationID string) error {
	if profileID == "" {
		return ErrProfileRequired
	}
	if _, err := s.db.Exec(`DELETE FROM ai_chat_org_workers WHERE profile_id = ? AND conversation_id = ?`, profileID, conversationID); err != nil {
		return fmt.Errorf("forget org workers: %w", err)
	}
	return nil
}
