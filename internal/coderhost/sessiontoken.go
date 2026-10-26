package coderhost

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// A session token is the caller identity of a coder turn the host started: it
// names one profile, one conversation and one turn, and tells the master tools
// (CLI verbs and MCP tools) who is calling. The tools take no "caller" argument,
// so a session can only act as the session whose token it holds.
//
// Why this is not an internal/agenttokens token: those authenticate against the
// profile's HTTP API (and may be issued with `agent-mode token create`), so a
// session token stored there would also open API routes, and its binding names a
// task, not a session. Same shape, separate table: a random secret, only its
// SHA-256 stored (agenttokens.Hash's scheme), checked by lookup.
//
// Binding:
//   - Only the host mints one (IssueSessionToken, called from the launcher right
//     before it starts a turn); no CLI verb or tool mints or returns a token.
//   - It is valid only while the turn it was minted for is the session's current
//     turn (coder_sessions.turn_id), so a token read from an old log or process
//     listing is dead once the turn ends, and a new turn replaces the old token.
//   - It also expires (SessionTokenTTL) in case the host died before the turn row
//     was cleared.
//
// What it does not stop: a process of the same user that reads another turn's
// environment (/proc/<pid>/environ) or writes the database itself. As with the
// agent-kind tokens (internal/browserctl/kindtoken.go) this is a courtesy layer
// between agents of one user, not a sandbox against that user.

// SessionTokenEnv is the variable the host hands a turn its session token in. The
// turn keeps it in its environment (see internal/cli/chat/chat_run.go): the shell
// and the MCP server its agent runtime starts need it to call the master tools.
const SessionTokenEnv = "MONOAGENT_SESSION_TOKEN"

// SessionTokenPrefix starts every session token, so a secret scanner can tell one.
const SessionTokenPrefix = "mst-"

// SessionTokenTTL is the longest a token lives.
const SessionTokenTTL = 24 * time.Hour

// ErrSessionToken is every failure to identify a caller: absent, unknown, expired
// or belonging to a turn that is no longer the session's current one.
var ErrSessionToken = errors.New("invalid session token: this call is not from a running coder session")

// Caller is who a token names.
type Caller struct {
	ProfileID      string
	ConversationID string
	TurnID         string
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// IssueSessionToken mints the token of the turn turnID of a session and replaces
// the tokens the session had. Only the host calls it.
func (s *Store) IssueSessionToken(profileID, conversationID, turnID string) (string, error) {
	if profileID == "" || conversationID == "" || turnID == "" {
		return "", errors.New("coder host: a session token needs a profile, a conversation and a turn")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("coder host: generating a session token: %w", err)
	}
	tok := SessionTokenPrefix + base64.RawURLEncoding.EncodeToString(b)
	now := s.now().UTC()
	if _, err := s.db.Exec(`DELETE FROM coder_session_tokens WHERE (profile_id = ? AND conversation_id = ?) OR expires_at < ?`,
		profileID, conversationID, now.Format(timeLayout)); err != nil {
		return "", err
	}
	_, err := s.db.Exec(`INSERT INTO coder_session_tokens (token_hash, profile_id, conversation_id, turn_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(tok), profileID, conversationID, turnID, now.Format(timeLayout), now.Add(SessionTokenTTL).Format(timeLayout))
	return tok, err
}

// RevokeSessionTokens drops a session's tokens (its turn ended).
func (s *Store) RevokeSessionTokens(profileID, conversationID string) {
	_, _ = s.db.Exec(`DELETE FROM coder_session_tokens WHERE profile_id = ? AND conversation_id = ?`, profileID, conversationID)
}

// VerifySessionToken says who the token names. It fails (ErrSessionToken, with no
// hint which check failed) unless the token is live and its turn is the session's
// current turn.
func (s *Store) VerifySessionToken(tok string) (Caller, error) {
	if !strings.HasPrefix(tok, SessionTokenPrefix) || len(tok) > 128 {
		return Caller{}, ErrSessionToken
	}
	var c Caller
	var expires string
	err := s.db.QueryRow(`SELECT t.profile_id, t.conversation_id, t.turn_id, t.expires_at FROM coder_session_tokens t
		JOIN coder_sessions x ON x.profile_id = t.profile_id AND x.conversation_id = t.conversation_id AND x.turn_id = t.turn_id
		WHERE t.token_hash = ?`, hashToken(tok)).Scan(&c.ProfileID, &c.ConversationID, &c.TurnID, &expires)
	if err != nil {
		return Caller{}, ErrSessionToken
	}
	if exp, perr := time.Parse(timeLayout, expires); perr != nil || !s.now().Before(exp) {
		return Caller{}, ErrSessionToken
	}
	return c, nil
}

// CallerFor identifies the caller of a master tool from the token it presents and the
// profile the call names. A token is for one profile: a call that names another is
// refused like a bad token.
func (s *Store) CallerFor(token, profileID string) (Caller, error) {
	c, err := s.VerifySessionToken(token)
	if err != nil {
		return Caller{}, err
	}
	if profileID != "" && profileID != c.ProfileID {
		return Caller{}, ErrSessionToken
	}
	return c, nil
}
