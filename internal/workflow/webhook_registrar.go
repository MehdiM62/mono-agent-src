package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// App webhook auto-registration. Some apps only send events to a URL that
// was registered through their API (Typeform, Calendly, Tally, Facebook Lead
// Ads, …). A WebhookRegistrar does that registration: the trigger manager
// calls Register when the workflow is activated and Unregister when it is
// deactivated, deleted or its trigger changes, and the hook's id is kept in
// the app_webhooks table so any process can remove it later.
//
// A registrar is found by name, either
//
//   - a node type (e.g. "trigger.tally"): every node of that type is served
//     like a trigger.webhook at /webhook/<path> (config "path", or one
//     derived from the workflow and node ids) and registered on activation;
//   - or a trigger.webhook's "register_with" config (e.g. "typeform"), an
//     opt-in that leaves every other trigger.webhook as it is.
//
// The vendor must reach the URL, so its base comes from the node's
// "public_url" or MONOAGENT_PUBLIC_URL (e.g. a tunnel); activation fails
// with an explanation when neither is set.

// AppWebhook is one trigger node's webhook as a registrar sees it.
type AppWebhook struct {
	WorkflowID string
	NodeID     string
	NodeType   string
	// Config is the node config; for trigger.webhook the entries of
	// "register_config" are merged on top. The node's credential
	// (credential_id) is resolved into Config["credential"].
	Config map[string]interface{}
	// URL is the public URL the vendor must call; Path its /webhook/ path.
	URL  string
	Path string
	// Secret is random and stable per node, for vendors that sign their
	// deliveries with a secret given at registration.
	Secret string
}

// AppWebhookHook is what the vendor returned for a registration.
type AppWebhookHook struct {
	ID    string
	Extra map[string]interface{}
	// ExpiresAt is when the vendor drops the hook; zero means never. A
	// registrar whose hooks expire sets it and implements WebhookRenewer.
	ExpiresAt time.Time
}

// WebhookRegistrar registers and removes a webhook with an app's API.
// Unregister should treat a hook the vendor no longer has as removed.
type WebhookRegistrar interface {
	Register(ctx context.Context, hook AppWebhook) (AppWebhookHook, error)
	Unregister(ctx context.Context, hook AppWebhook, stored AppWebhookHook) error
}

// WebhookChecker is optionally implemented by a registrar that can tell
// whether a stored hook still exists; an activation that finds one gone
// registers again instead of reusing it.
type WebhookChecker interface {
	Exists(ctx context.Context, hook AppWebhook, stored AppWebhookHook) (bool, error)
}

// WebhookVerifier is optionally implemented by a registrar whose vendor
// signs deliveries; a delivery it refuses is answered 401 and starts no run.
type WebhookVerifier interface {
	VerifyDelivery(header http.Header, body []byte, hook AppWebhook, stored AppWebhookHook) error
}

// WebhookRenewer is optionally implemented by a registrar whose vendor
// lets hooks expire (Microsoft Graph subscriptions, Jira Cloud webhooks).
// The daemon calls Renew before a stored hook's ExpiresAt (webhook_renew.go)
// and keeps the expiry it returns.
type WebhookRenewer interface {
	Renew(ctx context.Context, hook AppWebhook, stored AppWebhookHook) (time.Time, error)
}

var webhookRegistrars = struct {
	sync.RWMutex
	m map[string]WebhookRegistrar
}{m: map[string]WebhookRegistrar{}}

// RegisterWebhookRegistrar makes r the registrar for name: a node type, or
// a value of trigger.webhook's "register_with". A later call replaces it.
func RegisterWebhookRegistrar(name string, r WebhookRegistrar) {
	webhookRegistrars.Lock()
	defer webhookRegistrars.Unlock()
	webhookRegistrars.m[name] = r
}

// WebhookRegistrarFor returns the registrar registered for name, or nil.
func WebhookRegistrarFor(name string) WebhookRegistrar {
	webhookRegistrars.RLock()
	defer webhookRegistrars.RUnlock()
	return webhookRegistrars.m[name]
}

// appWebhookRecord is one app_webhooks row.
type appWebhookRecord struct {
	WorkflowID string
	NodeID     string
	Registrar  string
	HookID     string
	URL        string
	Secret     string
	Config     map[string]interface{} // without credential data
	ConfigHash string
	Extra      map[string]interface{}
	// ExpiresAt (zero: never) and RenewedAt, when it was set; RenewError
	// and RenewErrorClass keep the last failed renewal.
	ExpiresAt       time.Time
	RenewedAt       time.Time
	RenewError      string
	RenewErrorClass string
}

// appWebhookStore keeps the records; nil DB keeps them in memory.
type appWebhookStore struct {
	db  *sql.DB
	mu  sync.Mutex
	mem map[string]appWebhookRecord
}

func appWebhookKey(wf, node string) string { return wf + "\x00" + node }

func (s *appWebhookStore) get(ctx context.Context, wf, node string) (*appWebhookRecord, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.mem[appWebhookKey(wf, node)]
		if !ok {
			return nil, nil
		}
		return &rec, nil
	}
	rec := appWebhookRecord{WorkflowID: wf, NodeID: node}
	var cfg, extra string
	var expires, renewed sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT registrar, hook_id, url, secret, config, config_hash, extra, expires_at, renewed_at, renew_error, renew_error_class
		 FROM app_webhooks WHERE workflow_id = ? AND node_id = ?`,
		wf, node).Scan(&rec.Registrar, &rec.HookID, &rec.URL, &rec.Secret, &cfg, &rec.ConfigHash, &extra,
		&expires, &renewed, &rec.RenewError, &rec.RenewErrorClass)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("app webhooks: get: %w", err)
	}
	_ = json.Unmarshal([]byte(cfg), &rec.Config)
	_ = json.Unmarshal([]byte(extra), &rec.Extra)
	rec.ExpiresAt, rec.RenewedAt = expires.Time, renewed.Time
	return &rec, nil
}

// list returns workflow wf's records.
func (s *appWebhookStore) list(ctx context.Context, wf string) ([]appWebhookRecord, error) {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		var out []appWebhookRecord
		for _, rec := range s.mem {
			if rec.WorkflowID == wf {
				out = append(out, rec)
			}
		}
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT node_id FROM app_webhooks WHERE workflow_id = ?`, wf)
	if err != nil {
		return nil, fmt.Errorf("app webhooks: list: %w", err)
	}
	var nodes []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, fmt.Errorf("app webhooks: list: %w", err)
		}
		nodes = append(nodes, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("app webhooks: list: %w", err)
	}
	var out []appWebhookRecord
	for _, n := range nodes {
		rec, err := s.get(ctx, wf, n)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			out = append(out, *rec)
		}
	}
	return out, nil
}

func (s *appWebhookStore) put(ctx context.Context, rec appWebhookRecord) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.mem == nil {
			s.mem = make(map[string]appWebhookRecord)
		}
		s.mem[appWebhookKey(rec.WorkflowID, rec.NodeID)] = rec
		return nil
	}
	cfg, _ := json.Marshal(rec.Config)
	extra, _ := json.Marshal(rec.Extra)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO app_webhooks (workflow_id, node_id, registrar, hook_id, url, secret, config, config_hash, extra,
		   expires_at, renewed_at, renew_error, renew_error_class)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '')
		 ON CONFLICT (workflow_id, node_id) DO UPDATE SET registrar = excluded.registrar, hook_id = excluded.hook_id,
		   url = excluded.url, secret = excluded.secret, config = excluded.config,
		   config_hash = excluded.config_hash, extra = excluded.extra, created_at = CURRENT_TIMESTAMP,
		   expires_at = excluded.expires_at, renewed_at = excluded.renewed_at, renew_error = '', renew_error_class = ''`,
		rec.WorkflowID, rec.NodeID, rec.Registrar, rec.HookID, rec.URL, rec.Secret, string(cfg), rec.ConfigHash, string(extra),
		nullTime(rec.ExpiresAt), nullTime(rec.RenewedAt)); err != nil {
		return fmt.Errorf("app webhooks: put: %w", err)
	}
	return nil
}

// renewed records a hook's new expiry (or, with renewErr set, a failed
// renewal) on the row of that hook id; a row since removed or replaced is
// left alone.
func (s *appWebhookStore) renewed(ctx context.Context, wf, node, hookID string, expires, at time.Time, renewErr string, class ErrorClass) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		rec, ok := s.mem[appWebhookKey(wf, node)]
		if !ok || rec.HookID != hookID {
			return nil
		}
		if renewErr == "" {
			rec.ExpiresAt, rec.RenewedAt = expires, at
		}
		rec.RenewError, rec.RenewErrorClass = renewErr, string(class)
		s.mem[appWebhookKey(wf, node)] = rec
		return nil
	}
	var err error
	if renewErr == "" {
		_, err = s.db.ExecContext(ctx,
			`UPDATE app_webhooks SET expires_at = ?, renewed_at = ?, renew_error = '', renew_error_class = ''
			 WHERE workflow_id = ? AND node_id = ? AND hook_id = ?`, nullTime(expires), nullTime(at), wf, node, hookID)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE app_webhooks SET renew_error = ?, renew_error_class = ? WHERE workflow_id = ? AND node_id = ? AND hook_id = ?`,
			renewErr, string(class), wf, node, hookID)
	}
	if err != nil {
		return fmt.Errorf("app webhooks: renewed: %w", err)
	}
	return nil
}

// nullTime stores a zero time as NULL.
func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t.UTC(), Valid: !t.IsZero()}
}

func (s *appWebhookStore) delete(ctx context.Context, wf, node string) error {
	if s.db == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.mem, appWebhookKey(wf, node))
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM app_webhooks WHERE workflow_id = ? AND node_id = ?`, wf, node); err != nil {
		return fmt.Errorf("app webhooks: delete: %w", err)
	}
	return nil
}

// hashAppConfig identifies a registration's inputs: the node config (keys
// sorted by json.Marshal) and the URL.
func hashAppConfig(cfg map[string]interface{}, url string) string {
	raw, _ := json.Marshal(cfg)
	sum := sha256.Sum256(append(raw, url...))
	return hex.EncodeToString(sum[:])
}
