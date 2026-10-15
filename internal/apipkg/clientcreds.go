package apipkg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// clientFields names the credential fields holding the client id/secret.
func (o *OAuthSpec) clientFields() (string, string) {
	id, sec := o.ClientIDField, o.ClientSecretField
	if id == "" {
		id = "client_id"
	}
	if sec == "" {
		sec = "client_secret"
	}
	return id, sec
}

type ccToken struct {
	token   string
	expires time.Time
}

// ccCache holds client-credentials tokens by (token URL, client id, secret
// hash, scopes) until shortly before they expire.
var ccCache sync.Map

// clientCredentialsToken returns an access token from the OAuth
// client-credentials grant for credential c with the user's fields cred,
// fetching a new one when none is cached or the cached one is about to
// expire.
func clientCredentialsToken(ctx context.Context, c *Credential, cred map[string]any) (string, error) {
	o := c.OAuth
	sc := Scope{Cred: cred}
	tokenURL, err := renderText(o.TokenURL, sc)
	if err != nil {
		return "", fmt.Errorf("credential %s: token_url: %w", c.Platform, err)
	}
	idf, secf := o.clientFields()
	id, secret := stringify(cred[idf]), stringify(cred[secf])
	if id == "" || secret == "" {
		return "", fmt.Errorf("credential %s: the connection needs %s and %s", c.Platform, idf, secf)
	}
	sum := sha256.Sum256([]byte(secret))
	key := strings.Join([]string{tokenURL, id, hex.EncodeToString(sum[:8]), strings.Join(o.Scopes, " ")}, "\x00")
	if v, ok := ccCache.Load(key); ok {
		if t := v.(ccToken); time.Now().Before(t.expires) {
			return t.token, nil
		}
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	if len(o.Scopes) > 0 {
		form.Set("scope", strings.Join(o.Scopes, " "))
	}
	for k, tv := range o.ExtraParams {
		v, err := renderText(tv, sc)
		if err != nil {
			return "", fmt.Errorf("credential %s: extra_params.%s: %w", c.Platform, k, err)
		}
		if v != "" {
			form.Set(k, v)
		}
	}
	basic := o.ClientAuth == "basic"
	if !basic {
		form.Set("client_id", id)
		form.Set("client_secret", secret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("credential %s: token request: %w", c.Platform, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basic {
		req.SetBasicAuth(id, secret)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("credential %s: token request: %w", c.Platform, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyEcho))
	if err != nil {
		return "", fmt.Errorf("credential %s: token response: %w", c.Platform, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("credential %s: token request: HTTP %d: %s", c.Platform, resp.StatusCode, errorBodyEcho(body))
	}
	var tr struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return "", fmt.Errorf("credential %s: token response has no access_token", c.Platform)
	}
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if ttl > 2*time.Minute {
		ttl -= time.Minute // refresh a little early
	}
	ccCache.Store(key, ccToken{token: tr.AccessToken, expires: time.Now().Add(ttl)})
	return tr.AccessToken, nil
}
