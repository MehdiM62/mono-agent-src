package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/monoes/mono-agent/internal/secrets"
)

// The oauth_refresh_locks table (migration 095) backs RefreshToken's
// cross-process lock. The connections DB is shared (by file path) across
// the CLI, the daemon, and the GUI, each their own OS process with their own in-memory Store — a
// Go-level mutex only ever serializes goroutines inside one of them. Two
// processes resolving the same stale connection at once would otherwise both
// exchange the same stored refresh_token; providers that rotate single-use
// refresh tokens invalidate the loser's exchange, permanently breaking the
// connection. Coordinating through this table (which every process reads
// and writes through the same SQLite file) closes that gap.
//
// refreshLockStaleAfter bounds how long a lock row is honored before it's
// considered abandoned (its holder crashed or was killed mid-refresh) and
// safe to steal — a token exchange is a single HTTP round trip and never
// legitimately takes this long.
// A var so tests can pin it; production code never changes it.
var refreshLockStaleAfter = 30 * time.Second

// refreshLockWaitTimeout is how long a caller that lost the race waits for
// the current holder to finish before giving up and just using whatever is
// on file — a real exchange completes in well under this.
// A var so tests can pin it; production code never changes it.
var refreshLockWaitTimeout = 5 * time.Second

// ensureFreshToken checks if an OAuth token is expired and refreshes it using
// the stored refresh_token. Returns the (possibly refreshed) connection.
// Best-effort: any refresh failure is swallowed and the caller gets back the
// connection as-is (its existing, possibly-stale token) rather than an error,
// since callers on this path just want a usable connection to proceed with.
// Use RefreshToken instead when the caller needs to know whether it worked.
func (s *Store) ensureFreshToken(ctx context.Context, conn *Connection) (*Connection, error) {
	expiresStr, _ := conn.Data["expires_at"].(string)
	if expiresStr == "" {
		return conn, nil // Not an OAuth connection or no expiry — use as-is.
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresStr)
	if err != nil {
		return conn, nil // Can't parse — use as-is.
	}
	// Token still valid for at least 60 more seconds.
	if time.Now().UTC().Before(expiresAt.Add(-60 * time.Second)) {
		return conn, nil
	}
	_ = s.RefreshToken(ctx, conn)
	return conn, nil
}

// RefreshToken performs a refresh_token exchange for conn (regardless of
// whether its access_token has actually expired yet), persists the result,
// and reports exactly why it failed when it does — used by `connect refresh`
// and anywhere else that needs real feedback instead of a silent no-op.
func (s *Store) RefreshToken(ctx context.Context, conn *Connection) error {
	// Serialize refreshes across every process sharing this DB (CLI, daemon,
	// GUI) so overlapping resolvers don't each exchange the same stored
	// refresh_token. A caller that loses the race waits briefly for the
	// winner to finish, then re-reads and uses whatever it produced instead
	// of racing a second exchange of the same (possibly now-rotated) token.
	if conn.ID != "" {
		acquired, err := s.acquireRefreshLock(ctx, conn.ID)
		if err != nil {
			return fmt.Errorf("acquiring refresh lock: %w", err)
		}
		if !acquired {
			if latest, gerr := s.Get(ctx, conn.ID, conn.ProfileID); gerr == nil && latest != nil {
				*conn = *latest
			}
			return nil
		}
		defer s.releaseRefreshLock(context.WithoutCancel(ctx), conn.ID)

		// Re-read the latest tokens: a refresh that completed in another
		// process between our caller's Get and our lock acquisition may have
		// rotated the refresh_token, and exchanging the now-consumed one
		// would permanently break the connection.
		if latest, err := s.Get(ctx, conn.ID, conn.ProfileID); err == nil && latest != nil && latest.Data != nil {
			conn.Data = latest.Data
			conn.VaultRef = latest.VaultRef
		}
	}

	refreshToken, _ := conn.Data["refresh_token"].(string)
	if refreshToken == "" {
		return fmt.Errorf("no refresh_token stored for this connection — reconnect with the full OAuth flow instead")
	}

	p, ok := Get(conn.Platform)
	if !ok || p.OAuth == nil {
		return fmt.Errorf("platform %q has no OAuth config", conn.Platform)
	}

	cfg, err := tailorOAuth(*p.OAuth, conn.Data)
	if err != nil {
		return err
	}
	cfg.PlatformID = p.ID
	// Load client credentials from platform_oauth_credentials table (scoped
	// per profile — two connections for the same platform under different
	// profiles may need different Azure/OAuth app registrations), then fall
	// back to env vars.
	credProfileID := conn.ProfileID
	if credProfileID == "" {
		credProfileID = "default"
	}
	// Umbrella platforms also fall back to their per-product clients.
	if id, sec := s.lookupOAuthClient(ctx, p.ID, credProfileID); id != "" {
		cfg.ClientID, cfg.ClientSecret = id, sec
	}
	envPrefix := oauthEnvPrefix(p.ID)
	if cfg.ClientID == "" {
		return fmt.Errorf("missing OAuth client credentials for %q profile %q — run `monoagentcli connect set-oauth-client %s --client-id <id> [--client-secret <secret>]` or set %sCLIENT_ID", p.ID, credProfileID, p.ID, envPrefix)
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)

	body, status, err := PostTokenRequestWithAudienceFallback(cfg.PlatformID, cfg.TokenURL, form)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("token endpoint returned %d: %s", status, string(body))
	}

	var result OAuthResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	_ = json.Unmarshal(body, &result.Extra)
	if result.AccessToken == "" {
		return fmt.Errorf("token response missing access_token")
	}

	// Update connection data with new tokens.
	copyTokenFields(conn.Data, cfg.TokenFields, result.Extra)
	conn.Data["access_token"] = result.AccessToken
	conn.Data["token_type"] = result.TokenType
	if result.RefreshToken != "" {
		conn.Data["refresh_token"] = result.RefreshToken
	}
	if result.ExpiresIn > 0 {
		conn.Data["expires_at"] = time.Now().UTC().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	if IsUmbrella(getStr(conn.Data, "oauth_platform")) && result.Scope != "" {
		conn.Data["scope"] = result.Scope
	}
	conn.Status = "active"

	return s.Save(ctx, conn)
}

// acquireRefreshLock atomically claims the refresh lock for connID, stealing
// it if the existing holder's lock has gone stale (crashed mid-refresh), and
// otherwise polling briefly for the current holder to release it. Returns
// false (no error) if the lock is still held by someone else after
// refreshLockWaitTimeout — the caller is expected to fall back to reading
// whatever that holder produced instead of blocking forever.
func (s *Store) acquireRefreshLock(ctx context.Context, connID string) (bool, error) {
	deadline := time.Now().Add(refreshLockWaitTimeout)
	for {
		acquired, err := s.tryAcquireRefreshLock(ctx, connID)
		if err != nil {
			return false, err
		}
		if acquired {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// tryAcquireRefreshLock makes a single atomic attempt to claim connID's
// refresh lock. It succeeds if no lock row exists, or if the existing row is
// older than refreshLockStaleAfter (its holder is presumed dead). The
// INSERT...ON CONFLICT...WHERE form is a single statement, so SQLite's own
// locking (already tuned with busy_timeout in the DSN) makes the claim
// atomic across every process sharing this database file.
func (s *Store) tryAcquireRefreshLock(ctx context.Context, connID string) (bool, error) {
	now := time.Now().UTC()
	staleBefore := now.Add(-refreshLockStaleAfter).Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO oauth_refresh_locks (connection_id, locked_at) VALUES (?, ?)
		 ON CONFLICT(connection_id) DO UPDATE SET locked_at = excluded.locked_at
		 WHERE oauth_refresh_locks.locked_at < ?`,
		connID, now.Format(time.RFC3339), staleBefore)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// releaseRefreshLock drops connID's refresh lock so the next caller (in this
// process or another) doesn't have to wait out refreshLockStaleAfter.
func (s *Store) releaseRefreshLock(ctx context.Context, connID string) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM oauth_refresh_locks WHERE connection_id = ?`, connID)
}

// GetOAuthClient returns the OAuth app client_id/client_secret stored for
// platform under profileID (set via `connect set-oauth-client`, the GUI, or
// auto-persisted by a successful connect). Returns empty strings when none
// are stored (including when the table doesn't exist yet).
func (s *Store) GetOAuthClient(ctx context.Context, platform, profileID string) (clientID, clientSecret string) {
	if profileID == "" {
		profileID = "default"
	}
	var storedSecret string
	_ = s.db.QueryRowContext(ctx,
		`SELECT client_id, client_secret FROM platform_oauth_credentials WHERE platform = ? AND profile_id = ?`,
		platform, profileID,
	).Scan(&clientID, &storedSecret)
	if storedSecret != "" {
		// Decrypt; legacy plaintext rows pass through unchanged.
		if plain, err := secrets.DecryptBlob(ctx, s.db, profileID, storedSecret); err == nil {
			clientSecret = string(plain)
		}
	}
	return clientID, clientSecret
}

// SaveOAuthClient persists the OAuth app credentials for platform/profileID
// so silent token refresh keeps working in any later process without a
// MONOAGENT_<PLATFORM>_CLIENT_ID env var. Called automatically after every
// successful OAuth connect with whatever client credentials actually worked.
func (s *Store) SaveOAuthClient(ctx context.Context, platform, profileID, clientID, clientSecret string) error {
	if profileID == "" {
		profileID = "default"
	}
	// Encrypt the client_secret under the vault envelope (same as connection
	// data); an empty secret (PKCE-only platforms) stays empty.
	encSecret := clientSecret
	if clientSecret != "" {
		enc, encErr := secrets.EncryptBlob(ctx, s.db, profileID, []byte(clientSecret))
		if encErr != nil {
			return fmt.Errorf("connections.SaveOAuthClient: encrypting secret: %w", encErr)
		}
		encSecret = enc
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO platform_oauth_credentials (platform, profile_id, client_id, client_secret, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		platform, profileID, clientID, encSecret, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("connections.SaveOAuthClient: %w", err)
	}
	return nil
}
