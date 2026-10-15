package connections

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/monoes/mono-agent/internal/profiledir"
	"github.com/monoes/mono-agent/internal/secrets"

	"github.com/google/uuid"
)

// Connection represents a stored platform connection.
type Connection struct {
	ID         string                 `json:"id"`
	Platform   string                 `json:"platform"`
	Method     AuthMethod             `json:"method"`
	Label      string                 `json:"label"`
	AccountID  string                 `json:"account_id"`
	Data       map[string]interface{} `json:"data"`
	Status     string                 `json:"status"` // "active" | "expired" | "error"
	LastTested string                 `json:"last_tested,omitempty"`
	ProfileID  string                 `json:"profile_id,omitempty"`
	VaultRef   string                 `json:"vault_ref,omitempty"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
}

// SafeConnection is the credential-free projection of a Connection for any
// output boundary — CLI --json, GUI IPC responses — that must never leak
// stored token/secret material (Connection.Data holds access_token,
// refresh_token, api_key, etc. in cleartext).
type SafeConnection struct {
	ID         string     `json:"id"`
	Platform   string     `json:"platform"`
	Method     AuthMethod `json:"method"`
	Label      string     `json:"label"`
	AccountID  string     `json:"account_id"`
	Status     string     `json:"status"`
	LastTested string     `json:"last_tested,omitempty"`
	ProfileID  string     `json:"profile_id,omitempty"`
	VaultRef   string     `json:"vault_ref,omitempty"`
	CreatedAt  string     `json:"created_at"`
	UpdatedAt  string     `json:"updated_at"`
}

// Redact strips credential material, returning the safe-to-serialize view.
func (c Connection) Redact() SafeConnection {
	return SafeConnection{
		ID:         c.ID,
		Platform:   c.Platform,
		Method:     c.Method,
		Label:      c.Label,
		AccountID:  c.AccountID,
		Status:     c.Status,
		LastTested: c.LastTested,
		ProfileID:  c.ProfileID,
		VaultRef:   c.VaultRef,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
}

// RedactAll maps a slice of Connections to their safe projections.
func RedactAll(conns []Connection) []SafeConnection {
	out := make([]SafeConnection, len(conns))
	for i, c := range conns {
		out[i] = c.Redact()
	}
	return out
}

// Store provides CRUD operations for connections.
type Store struct {
	db *sql.DB
}

// NewStore creates a new Store backed by the given database.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Save upserts a connection. If ID is empty a new UUID is generated. If
// CreatedAt is empty it is set to now. UpdatedAt is always refreshed.
// Status defaults to "active" when empty.
func (s *Store) Save(ctx context.Context, c *Connection) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if c.CreatedAt == "" {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = "active"
	}
	if c.ProfileID == "" {
		return fmt.Errorf("connections.Save: %w", profiledir.ErrNoProfile)
	}

	secretFields, nonSecret := splitSecretFields(c.Platform, c.Method, c.Data)
	if len(secretFields) > 0 {
		linkID, putErr := secrets.PutSystemEntry(ctx, s.db, c.ProfileID, "connection", c.VaultRef, connectionVaultName(c), secretFields, c.AccountID, "")
		if putErr != nil {
			return fmt.Errorf("connections.Save: saving credentials to vault: %w", putErr)
		}
		c.VaultRef = linkID
	}

	dataBytes, err := json.Marshal(nonSecret)
	if err != nil {
		return fmt.Errorf("connections.Save: marshal data: %w", err)
	}
	encodedData, err := secrets.EncryptBlob(ctx, s.db, c.ProfileID, dataBytes)
	if err != nil {
		return fmt.Errorf("connections.Save: encrypting data: %w", err)
	}

	const q = `
INSERT INTO connections (id, platform, method, label, account_id, data, status, last_tested, profile_id, vault_ref, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    label=excluded.label, account_id=excluded.account_id,
    data=excluded.data, status=excluded.status,
    last_tested=excluded.last_tested, vault_ref=excluded.vault_ref, updated_at=excluded.updated_at`

	_, err = s.db.ExecContext(ctx, q,
		c.ID, c.Platform, string(c.Method), c.Label, c.AccountID,
		encodedData, c.Status, c.LastTested, c.ProfileID, c.VaultRef, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("connections.Save: %w", err)
	}
	return nil
}

// Get returns the connection with the given ID, or nil if not found.
//
// An optional profileID scopes the lookup to a single profile with the same
// profile_id = ? predicate Delete/List use (an empty profileID argument is
// profiledir.ErrNoProfile). Without it the lookup
// stays unscoped for legacy callers: cmd/monoagentcli/node.go's
// resolveCredentialData post-checks conn.ProfileID itself, and
// secret_export.go's rematerializer must re-read rows matched by
// ListByPlatform(profileID) whatever profile that is.
func (s *Store) Get(ctx context.Context, id string, profileID ...string) (*Connection, error) {
	q := `SELECT id, platform, method, label, account_id, data, status, last_tested, COALESCE(profile_id,''), COALESCE(vault_ref,''), created_at, updated_at
          FROM connections WHERE id = ?`
	args := []interface{}{id}
	if len(profileID) > 0 {
		p := profileID[0]
		if p == "" {
			return nil, profiledir.ErrNoProfile
		}
		q += ` AND profile_id = ?`
		args = append(args, p)
	}
	row := s.db.QueryRowContext(ctx, q, args...)
	return scanConnection(ctx, s.db, row)
}

// GetOrResolve looks up a connection by ID, falling back to platform name
// lookup if the ID isn't found. Both lookups are scoped to profileID: a
// workflow running under profile B must never resolve — and silently inject
// — a credential that only exists under profile A. An empty profileID is
// profiledir.ErrNoProfile. When several same-profile connections exist for
// a platform, active ones are preferred. OAuth tokens are refreshed if
// expired.
func (s *Store) GetOrResolve(ctx context.Context, idOrPlatform, profileID string) (*Connection, error) {
	if profileID == "" {
		return nil, profiledir.ErrNoProfile
	}

	// Try by ID first, scoped to the profile like Delete/List.
	conn, err := s.Get(ctx, idOrPlatform, profileID)
	if err == nil && conn != nil {
		return s.ensureFreshToken(ctx, conn)
	}

	// Fallback: look up by platform name, restricted to the same profile —
	// the previous all-profiles fallback was cross-profile credential
	// injection (RA4).
	conns, err := s.ListByPlatform(ctx, idOrPlatform, profileID)
	if err == nil && len(conns) == 0 {
		// A per-product platform (gmail, outlook, …) with no connection of
		// its own is served by its umbrella's (google, microsoft).
		if u := s.resolveViaUmbrella(ctx, idOrPlatform, profileID); u != nil {
			return s.ensureFreshToken(ctx, u)
		}
	}
	if err != nil || len(conns) == 0 {
		return nil, fmt.Errorf("no connection found for %q under profile %q", idOrPlatform, profileID)
	}
	// Prefer active connections.
	for i := range conns {
		if conns[i].Status == "active" {
			return s.ensureFreshToken(ctx, &conns[i])
		}
	}
	return s.ensureFreshToken(ctx, &conns[0])
}

// ListAll returns all connections ordered by platform then created_at.
func (s *Store) ListAll(ctx context.Context, profileID string) ([]Connection, error) {
	if profileID == "" {
		return nil, profiledir.ErrNoProfile
	}
	const q = `SELECT id, platform, method, label, account_id, data, status, last_tested, COALESCE(profile_id,''), COALESCE(vault_ref,''), created_at, updated_at
	           FROM connections WHERE profile_id = ? ORDER BY platform, created_at`
	rows, err := s.db.QueryContext(ctx, q, profileID)
	if err != nil {
		return nil, fmt.Errorf("connections.ListAll: %w", err)
	}
	defer rows.Close()
	out, err := scanConnections(ctx, s.db, rows)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []Connection{}
	}
	return out, nil
}

// ListByPlatform returns a profile's connections for a given platform. An empty
// profileID is profiledir.ErrNoProfile; it never means "every profile".
func (s *Store) ListByPlatform(ctx context.Context, platform, profileID string) ([]Connection, error) {
	if profileID == "" {
		return nil, profiledir.ErrNoProfile
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, platform, method, label, account_id, data, status, last_tested, COALESCE(profile_id,''), COALESCE(vault_ref,''), created_at, updated_at
		 FROM connections WHERE platform = ? AND profile_id = ? ORDER BY created_at`,
		platform, profileID)
	if err != nil {
		return nil, fmt.Errorf("connections.ListByPlatform: %w", err)
	}
	defer rows.Close()
	return scanConnections(ctx, s.db, rows)
}

// Delete removes a connection by ID, scoped to profileID, and its linked
// vault entry (if any) — same credential, so both must go together.
// Returns an error if the row does not exist.
func (s *Store) Delete(ctx context.Context, id, profileID string) error {
	if profileID == "" {
		return profiledir.ErrNoProfile
	}
	if conn, getErr := s.Get(ctx, id, profileID); getErr == nil && conn != nil && conn.VaultRef != "" {
		if delErr := secrets.Delete(ctx, s.db, profileID, conn.VaultRef); delErr != nil {
			fmt.Fprintf(os.Stderr, "warning: connection %s deleted but its vault entry %s could not be removed: %v\n", id, conn.VaultRef, delErr)
		}
	}
	const q = `DELETE FROM connections WHERE id = ? AND profile_id = ?`
	res, err := s.db.ExecContext(ctx, q, id, profileID)
	if err != nil {
		return fmt.Errorf("connections.Delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("connections.Delete: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("connections.Delete: id %q not found", id)
	}
	return nil
}

// MarkTested updates the status, last_tested, and updated_at fields for the
// given connection ID. An optional profileID scopes the update to a single
// profile with the same profile_id = ? predicate Delete/List use (an empty
// profileID argument is profiledir.ErrNoProfile);
// without it the update is unscoped for legacy callers whose call-site has
// no profile context available.
func (s *Store) MarkTested(ctx context.Context, id, status string, profileID ...string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	q := `UPDATE connections SET status = ?, last_tested = ?, updated_at = ? WHERE id = ?`
	args := []interface{}{status, now, now, id}
	if len(profileID) > 0 {
		p := profileID[0]
		if p == "" {
			return profiledir.ErrNoProfile
		}
		q += ` AND profile_id = ?`
		args = append(args, p)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("connections.MarkTested: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("connections.MarkTested: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("connections.MarkTested: id %q not found", id)
	}
	return nil
}

// scanConnection reads a single Connection from a *sql.Row.
func scanConnection(ctx context.Context, db *sql.DB, row *sql.Row) (*Connection, error) {
	var c Connection
	var dataJSON, method string
	err := row.Scan(&c.ID, &c.Platform, &method, &c.Label, &c.AccountID,
		&dataJSON, &c.Status, &c.LastTested, &c.ProfileID, &c.VaultRef, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scanConnection: %w", err)
	}
	c.Method = AuthMethod(method)
	decoded, err := secrets.DecryptBlob(ctx, db, c.ProfileID, dataJSON)
	if err != nil {
		return nil, fmt.Errorf("scanConnection: decrypting data: %w", err)
	}
	if err := json.Unmarshal(decoded, &c.Data); err != nil {
		return nil, fmt.Errorf("scanConnection: unmarshal data: %w", err)
	}
	if err := mergeVaultFields(ctx, db, &c); err != nil {
		return nil, fmt.Errorf("scanConnection: %w", err)
	}
	return &c, nil
}

// mergeVaultFields decrypts c's linked vault entry (if any) and merges its
// fields back into c.Data, so every existing caller reading e.g.
// c.Data["access_token"] keeps working regardless of where the value is
// actually stored.
func mergeVaultFields(ctx context.Context, db *sql.DB, c *Connection) error {
	if c.VaultRef == "" {
		return nil
	}
	resolved, _, err := secrets.DecryptFields(ctx, db, c.ProfileID, c.VaultRef)
	if err != nil {
		return fmt.Errorf("resolving vault credentials: %w", err)
	}
	if c.Data == nil {
		c.Data = map[string]interface{}{}
	}
	for k, v := range resolved {
		c.Data[k] = v
	}
	return nil
}

// scanConnections reads all Connection rows from a *sql.Rows result set. A
// per-row mergeVaultFields failure (e.g. a dangling vault_ref left by an
// out-of-band delete or corruption) is logged and that row is omitted,
// rather than aborting the whole scan — unlike scanConnection's single-row
// path, which does return the error, since a caller looking up one specific
// connection should see it fail clearly instead of silently disappearing.
func scanConnections(ctx context.Context, db *sql.DB, rows *sql.Rows) ([]Connection, error) {
	var out []Connection
	for rows.Next() {
		var c Connection
		var method, dataJSON string
		if err := rows.Scan(
			&c.ID, &c.Platform, &method, &c.Label, &c.AccountID,
			&dataJSON, &c.Status, &c.LastTested, &c.ProfileID, &c.VaultRef, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanConnections: %w", err)
		}
		c.Method = AuthMethod(method)
		decoded, err := secrets.DecryptBlob(ctx, db, c.ProfileID, dataJSON)
		if err != nil {
			return nil, fmt.Errorf("scanConnections: decrypting data: %w", err)
		}
		if err := json.Unmarshal(decoded, &c.Data); err != nil {
			return nil, fmt.Errorf("scanConnections: unmarshal data: %w", err)
		}
		if err := mergeVaultFields(ctx, db, &c); err != nil {
			fmt.Fprintf(os.Stderr, "warning: connection %s: could not resolve vault credentials, omitting from list: %v\n", c.ID, err)
			continue
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanConnections: rows: %w", err)
	}
	return out, nil
}
