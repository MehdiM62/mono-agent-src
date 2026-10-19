package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// oauthEnvPrefix is the env-var prefix for a platform's OAuth client:
// MONOAGENT_<PLATFORM>_ (CLIENT_ID / CLIENT_SECRET).
func oauthEnvPrefix(platformID string) string {
	return "MONOAGENT_" + strings.ToUpper(strings.ReplaceAll(platformID, "-", "_")) + "_"
}

// lookupOAuthClient finds the OAuth app client for platformID under
// profileID. Order: the client stored for the platform (its secret falling
// back to MONOAGENT_<PLATFORM>_CLIENT_SECRET), then the platform's env vars.
// For an umbrella platform it then falls back to the legacy per-product
// clients so an existing setup needs no new client registration: first the
// per-product env vars (MONOAGENT_GOOGLE_SHEETS_, _GMAIL_, _GOOGLE_DRIVE_,
// _YOUTUBE_; MONOAGENT_OUTLOOK_ for microsoft), then the clients stored for
// those platforms. A client id and its secret always come from one source.
func (s *Store) lookupOAuthClient(ctx context.Context, platformID, profileID string) (clientID, clientSecret string) {
	prefix := oauthEnvPrefix(platformID)
	if id, sec := s.GetOAuthClient(ctx, platformID, profileID); id != "" {
		if sec == "" {
			sec = os.Getenv(prefix + "CLIENT_SECRET")
		}
		return id, sec
	}
	if id := os.Getenv(prefix + "CLIENT_ID"); id != "" {
		return id, os.Getenv(prefix + "CLIENT_SECRET")
	}
	members := legacyMembers(platformID)
	for _, legacy := range members {
		lp := oauthEnvPrefix(legacy)
		if id := os.Getenv(lp + "CLIENT_ID"); id != "" {
			return id, os.Getenv(lp + "CLIENT_SECRET")
		}
	}
	for _, legacy := range members {
		if id, sec := s.GetOAuthClient(ctx, legacy, profileID); id != "" {
			return id, sec
		}
	}
	return "", ""
}

// ListUsableByPlatform returns the profile's connections a node whose
// credential_platform is platform can use: the platform's own connections
// first, then its umbrella's (gmail → gmail + google).
func (s *Store) ListUsableByPlatform(ctx context.Context, platform, profileID string) ([]Connection, error) {
	var out []Connection
	for _, p := range CompatiblePlatforms(platform) {
		conns, err := s.ListByPlatform(ctx, p, profileID)
		if err != nil {
			return nil, err
		}
		out = append(out, conns...)
	}
	return out, nil
}

// PinnedID is the id of the connection idOrPlatform names, chosen as the
// CLI resolves a credential: by id in the profile, else the platform's
// first active usable connection, else its first. "" when there is none.
// A send card pins it (credential_id), so an approval sends through the
// connection the run chose.
func (s *Store) PinnedID(ctx context.Context, idOrPlatform, profileID string) string {
	if c, err := s.Get(ctx, idOrPlatform, profileID); err == nil && c != nil {
		return c.ID
	}
	conns, err := s.ListUsableByPlatform(ctx, idOrPlatform, profileID)
	if err != nil || len(conns) == 0 {
		return ""
	}
	for _, c := range conns {
		if c.Status == "active" {
			return c.ID
		}
	}
	return conns[0].ID
}

// pickUmbrellaConnection chooses, among umbrella connections, the one to
// serve a node of credential platform product: active and granted the
// product's scopes first, then any active one, then the first.
func pickUmbrellaConnection(conns []Connection, product string) *Connection {
	for i := range conns {
		if conns[i].Status == "active" && CheckCredentialScopes(conns[i].Data, product) == nil {
			return &conns[i]
		}
	}
	for i := range conns {
		if conns[i].Status == "active" {
			return &conns[i]
		}
	}
	if len(conns) > 0 {
		return &conns[0]
	}
	return nil
}

// resolveViaUmbrella is GetOrResolve's last resort for a per-product
// platform name with no connection of its own: the umbrella's connections.
func (s *Store) resolveViaUmbrella(ctx context.Context, platform, profileID string) *Connection {
	umbrella, ok := UmbrellaFor(platform)
	if !ok {
		return nil
	}
	conns, err := s.ListByPlatform(ctx, umbrella, profileID)
	if err != nil {
		return nil
	}
	return pickUmbrellaConnection(conns, platform)
}

// prepareUmbrellaOAuth narrows an umbrella platform's requested scopes to
// the selected groups. Scope groups on any other platform are an error.
func prepareUmbrellaOAuth(cfg *OAuthConfig, platformID string, groups []string) error {
	if !IsUmbrella(platformID) {
		if len(nonEmpty(groups)) > 0 {
			return fmt.Errorf("--scopes applies only to the %s platforms, not %q", strings.Join(umbrellaOrder, " and "), platformID)
		}
		return nil
	}
	scopes, err := ResolveScopes(platformID, groups)
	if err != nil {
		return err
	}
	cfg.Scopes = scopes
	return nil
}

// recordUmbrellaGrant marks a freshly consented umbrella connection so nodes
// can check its scopes, keeping the requested scopes when the provider's
// token response omits the granted list.
func recordUmbrellaGrant(conn *Connection, platformID string, requested []string) {
	if !IsUmbrella(platformID) {
		return
	}
	conn.Data["oauth_platform"] = platformID
	if s, _ := conn.Data["scope"].(string); strings.TrimSpace(s) == "" {
		conn.Data["scope"] = strings.Join(requested, " ")
	}
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// Account-identity endpoints for umbrella validation; vars so tests can
// point them at an httptest server.
var (
	googleUserinfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
	microsoftMeURL    = "https://graph.microsoft.com/v1.0/me?$select=mail,userPrincipalName"
)

// validateGoogleAccount resolves a google umbrella connection's account email
// from the OpenID userinfo endpoint (the openid + userinfo.email scopes are
// always requested, whatever groups were chosen).
func validateGoogleAccount(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	if token == "" {
		return "", fmt.Errorf("validateGoogleAccount: missing access_token")
	}
	body, status, err := doGET(ctx, googleUserinfoURL, "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateGoogleAccount: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateGoogleAccount: %w", &StatusError{Code: status})
	}
	var r struct {
		Email string `json:"email"`
		Sub   string `json:"sub"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("validateGoogleAccount: parse response: %w", err)
	}
	if r.Email != "" {
		return r.Email, nil
	}
	return r.Sub, nil
}

// validateMicrosoftAccount resolves a microsoft umbrella connection's
// account from Graph /me (User.Read is always requested).
func validateMicrosoftAccount(ctx context.Context, c *Connection) (string, error) {
	token := getStr(c.Data, "access_token")
	if token == "" {
		return "", fmt.Errorf("validateMicrosoftAccount: missing access_token")
	}
	body, status, err := doGET(ctx, microsoftMeURL, "Bearer "+token)
	if err != nil {
		return "", fmt.Errorf("validateMicrosoftAccount: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("validateMicrosoftAccount: %w", &StatusError{Code: status})
	}
	var r struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("validateMicrosoftAccount: parse response: %w", err)
	}
	if r.Mail != "" {
		return r.Mail, nil
	}
	return r.UserPrincipalName, nil
}

// adoptSameAccountConnection gives conn the identity (ID, vault link,
// created time, label) of the profile's existing connection on the same
// platform and account, so saving it updates that row in place.
func (m *Manager) adoptSameAccountConnection(ctx context.Context, conn *Connection, accountID string) {
	if accountID == "" || conn.ProfileID == "" {
		return
	}
	existing, err := m.store.ListByPlatform(ctx, conn.Platform, conn.ProfileID)
	if err != nil {
		return
	}
	for _, e := range existing {
		if e.Method == conn.Method && strings.EqualFold(e.AccountID, accountID) {
			conn.ID = e.ID
			conn.VaultRef = e.VaultRef
			conn.CreatedAt = e.CreatedAt
			return
		}
	}
}

// ListUsable returns the profile's connections usable by a node whose
// credential_platform is platform, including its umbrella's connections;
// an empty platform lists every connection.
func (m *Manager) ListUsable(ctx context.Context, platform, profileID string) ([]Connection, error) {
	if platform == "" {
		return m.List(ctx, "", profileID)
	}
	conns, err := m.store.ListUsableByPlatform(ctx, platform, profileID)
	if err != nil {
		return nil, err
	}
	if conns == nil {
		conns = []Connection{}
	}
	return conns, nil
}

// ScopeGroupsHelp is a one-line summary of the umbrella scope groups, for
// CLI flag help. Opt-in groups are listed after "opt-in:"; a plain connect
// grants the others.
func ScopeGroupsHelp() string {
	var parts []string
	for _, id := range umbrellaOrder {
		var names, optIn []string
		for _, g := range umbrellas[id].Groups {
			if g.OptIn {
				optIn = append(optIn, g.Name)
			} else {
				names = append(names, g.Name)
			}
		}
		part := id + ": " + strings.Join(names, ",")
		if len(optIn) > 0 {
			part += " (opt-in, granted only when named or with all: " + strings.Join(optIn, ",") + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}
