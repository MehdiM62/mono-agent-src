package workflow

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// appHookTimeout bounds one registrar call.
const appHookTimeout = 30 * time.Second

// CredentialResolver returns a connection's data by id or platform name,
// within a profile.
type CredentialResolver func(ctx context.Context, profileID, idOrPlatform string) (map[string]interface{}, error)

// appWebhookDeps is what the trigger manager needs for app webhooks.
type appWebhookDeps struct {
	store   *appWebhookStore
	resolve CredentialResolver
}

// SetAppWebhooks keeps app webhook records in db (nil: in memory) and
// resolves node credentials through resolve.
func (tm *TriggerManager) SetAppWebhooks(db *sql.DB, resolve CredentialResolver) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.apps = appWebhookDeps{store: &appWebhookStore{db: db}, resolve: resolve}
}

func (tm *TriggerManager) appDeps() appWebhookDeps {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.apps
}

// appHookState is an active node's registration, kept on its trigger entry.
type appHookState struct {
	name       string
	workflowID string
	nodeID     string
	profileID  string
}

// appVerifier checks deliveries against the registration. The route is
// registered before the app is called, so it is filled in afterwards; the
// app sends nothing before it has the registration.
type appVerifier struct {
	mu     sync.Mutex
	v      WebhookVerifier
	hook   AppWebhook
	stored AppWebhookHook
}

func (a *appVerifier) set(r WebhookRegistrar, hook AppWebhook, stored AppWebhookHook) {
	v, ok := r.(WebhookVerifier)
	if !ok {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v, a.hook, a.stored = v, hook, stored
}

func (a *appVerifier) check(h http.Header, body []byte) error {
	a.mu.Lock()
	v, hook, stored := a.v, a.hook, a.stored
	a.mu.Unlock()
	if v == nil {
		return nil
	}
	return v.VerifyDelivery(h, body, hook, stored)
}

// appHookPath is the webhook path of a registrar node type without a
// "path": stable per workflow and node, so re-activation keeps the URL.
func appHookPath(workflowID, nodeID string) string {
	sum := sha256.Sum256([]byte(workflowID + "\x00" + nodeID))
	return "app-" + hex.EncodeToString(sum[:8])
}

// appRegistrarFor returns the registrar a node uses and its name; nil when
// the node has none.
func appRegistrarFor(node *WorkflowNode) (string, WebhookRegistrar, error) {
	if node.Type != TriggerNodeTypeWebhook {
		return node.Type, WebhookRegistrarFor(node.Type), nil
	}
	name, _ := node.Config["register_with"].(string)
	if name = strings.TrimSpace(name); name == "" {
		return "", nil, nil
	}
	r := WebhookRegistrarFor(name)
	if r == nil {
		return "", nil, fmt.Errorf("trigger.webhook: register_with %q: no app registrar has that name", name)
	}
	return name, r, nil
}

// appHookConfig is the config a registrar sees: the node config with
// register_config's entries on top.
func appHookConfig(node *WorkflowNode) map[string]interface{} {
	cfg := make(map[string]interface{}, len(node.Config))
	for k, v := range node.Config {
		if k != "register_config" {
			cfg[k] = v
		}
	}
	extra, _ := node.Config["register_config"].(map[string]interface{})
	if raw, ok := node.Config["register_config"].(string); ok && strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &extra) // a code field holds JSON text
	}
	if extra != nil {
		for k, v := range extra {
			cfg[k] = v
		}
	}
	return cfg
}

// publicWebhookBase is the base URL the vendor reaches the webhook server at:
// the node's public_url, else PublicBaseURL when MONOAGENT_PUBLIC_URL is set.
func publicWebhookBase(cfg map[string]interface{}) (string, error) {
	base, _ := cfg["public_url"].(string)
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	// PublicBaseURL falls back to the listen address, which an app on the
	// internet cannot reach, so only MONOAGENT_PUBLIC_URL counts here.
	if base == "" && strings.TrimSpace(os.Getenv(publicURLEnv)) != "" {
		base = PublicBaseURL()
	}
	if base == "" {
		return "", fmt.Errorf("the app must reach this webhook from the internet: set the node's public_url or %s to the public base URL of the webhook server (e.g. a tunnel to 127.0.0.1:9321)", publicURLEnv)
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("public URL %q is not an http(s) URL", base)
	}
	return base, nil
}

// withCredential resolves cfg's credential_id into cfg["credential"].
func (tm *TriggerManager) withCredential(ctx context.Context, profileID string, cfg map[string]interface{}) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(cfg)+1)
	for k, v := range cfg {
		out[k] = v
	}
	id, _ := cfg["credential_id"].(string)
	if id == "" {
		return out, nil
	}
	resolve := tm.appDeps().resolve
	if resolve == nil {
		return nil, fmt.Errorf("credential %q: no credential store in this process", id)
	}
	data, err := resolve(ctx, profileID, id)
	if err != nil {
		return nil, fmt.Errorf("credential %q: %w", id, err)
	}
	out["credential"] = data
	return out, nil
}

func randomSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func workflowProfile(w *Workflow) string {
	if w.ProfileID == "" {
		return "default"
	}
	return w.ProfileID
}

// activateWebhookNode serves a trigger.webhook (or registrar node type) and,
// when it has an app registrar, registers it with the app. A registration
// that fails removes the route again.
func (tm *TriggerManager) activateWebhookNode(ctx context.Context, w *Workflow, node *WorkflowNode) error {
	if node.Config == nil {
		if err := node.ParseConfig(); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	name, r, err := appRegistrarFor(node)
	if err != nil {
		return err
	}
	if r == nil {
		_, _, err := tm.registerWebhookRoute(w.ID, node, "", nil)
		return err
	}
	defaultPath := ""
	if node.Type != TriggerNodeTypeWebhook {
		defaultPath = appHookPath(w.ID, node.ID)
	}
	verifier := &appVerifier{}
	path, added, err := tm.registerWebhookRoute(w.ID, node, defaultPath, verifier.check)
	if err != nil || !added {
		return err
	}
	st, err := tm.registerAppHook(ctx, w, node, name, r, path, verifier)
	if err != nil {
		tm.mu.Lock()
		if e := tm.active[w.ID][node.ID]; e != nil && e.webhookPath == path {
			tm.deactivateEntry(node.ID, e)
			delete(tm.active[w.ID], node.ID)
		}
		tm.mu.Unlock()
		return fmt.Errorf("register the webhook with %s: %w", name, err)
	}
	tm.mu.Lock()
	if e := tm.active[w.ID][node.ID]; e != nil && e.webhookPath == path {
		e.app = st
		tm.mu.Unlock()
		return nil
	}
	tm.mu.Unlock()
	// Deactivated while registering: undo the registration.
	tm.unregisterAppHook(st)
	return nil
}

// registerAppHook registers the node's webhook with the app, or reuses the
// stored registration when nothing it depends on changed (and, for a
// WebhookChecker, the app still has it).
func (tm *TriggerManager) registerAppHook(ctx context.Context, w *Workflow, node *WorkflowNode, name string, r WebhookRegistrar, path string, verifier *appVerifier) (*appHookState, error) {
	ctx, cancel := context.WithTimeout(ctx, appHookTimeout)
	defer cancel()
	cfg := appHookConfig(node)
	base, err := publicWebhookBase(cfg)
	if err != nil {
		return nil, err
	}
	profile := workflowProfile(w)
	st := &appHookState{name: name, workflowID: w.ID, nodeID: node.ID, profileID: profile}
	hook := AppWebhook{WorkflowID: w.ID, NodeID: node.ID, NodeType: node.Type, URL: base + "/webhook/" + path, Path: path}
	hash := hashAppConfig(cfg, hook.URL)
	if hook.Config, err = tm.withCredential(ctx, profile, cfg); err != nil {
		return nil, err
	}

	store := tm.appDeps().store
	rec, err := store.get(ctx, w.ID, node.ID)
	if err != nil {
		return nil, err
	}
	hook.Secret = randomSecret()
	if rec != nil && rec.Secret != "" {
		hook.Secret = rec.Secret
	}
	expired := rec != nil && !rec.ExpiresAt.IsZero() && !time.Now().Before(rec.ExpiresAt)
	if rec != nil && rec.Registrar == name && rec.ConfigHash == hash && rec.HookID != "" && !expired {
		stored := AppWebhookHook{ID: rec.HookID, Extra: rec.Extra, ExpiresAt: rec.ExpiresAt}
		keep := true
		if c, ok := r.(WebhookChecker); ok {
			exists, err := c.Exists(ctx, hook, stored)
			if err != nil {
				tm.logger.Warn().Err(err).Str("workflow_id", w.ID).Str("registrar", name).Msg("app webhook: checking the stored hook failed; reusing it")
			} else {
				keep = exists
			}
		}
		if keep {
			verifier.set(r, hook, stored)
			tm.logger.Info().Str("workflow_id", w.ID).Str("node_id", node.ID).Str("registrar", name).Msg("app webhook: stored registration reused")
			return st, nil
		}
	} else if rec != nil {
		// Registered with other inputs (or another app), or expired:
		// remove that one.
		tm.unregisterRecord(ctx, profile, rec)
	}

	got, err := r.Register(ctx, hook)
	if err != nil {
		return nil, err
	}
	err = store.put(ctx, appWebhookRecord{
		WorkflowID: w.ID, NodeID: node.ID, Registrar: name, HookID: got.ID, URL: hook.URL,
		Secret: hook.Secret, Config: cfg, ConfigHash: hash, Extra: got.Extra,
		ExpiresAt: got.ExpiresAt, RenewedAt: renewedAt(got.ExpiresAt),
	})
	if err != nil {
		// Without the id nothing could remove the hook later.
		if uerr := r.Unregister(ctx, hook, got); uerr != nil {
			err = errors.Join(err, fmt.Errorf("removing the new hook %q: %w", got.ID, uerr))
		}
		return nil, err
	}
	verifier.set(r, hook, got)
	tm.logger.Info().Str("workflow_id", w.ID).Str("node_id", node.ID).Str("registrar", name).Str("hook_id", got.ID).Msg("app webhook registered")
	return st, nil
}

// renewedAt is the RenewedAt of a hook just given expiry expires: now, or
// zero for a hook that does not expire.
func renewedAt(expires time.Time) time.Time {
	if expires.IsZero() {
		return time.Time{}
	}
	return time.Now()
}

// unregisterAppHook removes a deactivated node's registration from the app.
func (tm *TriggerManager) unregisterAppHook(st *appHookState) {
	ctx, cancel := context.WithTimeout(context.Background(), appHookTimeout)
	defer cancel()
	rec, err := tm.appDeps().store.get(ctx, st.workflowID, st.nodeID)
	if err != nil {
		tm.logger.Error().Err(err).Str("workflow_id", st.workflowID).Msg("app webhook: reading the stored hook")
		return
	}
	if rec != nil {
		tm.unregisterRecord(ctx, st.profileID, rec)
	}
}

// unregisterRecord calls the record's registrar to remove the hook, then
// forgets the record. A failure is logged with the hook id, so it can be
// removed by hand in the app.
func (tm *TriggerManager) unregisterRecord(ctx context.Context, profileID string, rec *appWebhookRecord) {
	log := tm.logger.With().Str("workflow_id", rec.WorkflowID).Str("node_id", rec.NodeID).
		Str("registrar", rec.Registrar).Str("hook_id", rec.HookID).Logger()
	err := func() error {
		r := WebhookRegistrarFor(rec.Registrar)
		if r == nil {
			return fmt.Errorf("no app registrar named %q", rec.Registrar)
		}
		cfg, err := tm.withCredential(ctx, profileID, rec.Config)
		if err != nil {
			return err
		}
		path := rec.URL[strings.LastIndex(rec.URL, "/webhook/")+len("/webhook/"):]
		hook := AppWebhook{WorkflowID: rec.WorkflowID, NodeID: rec.NodeID, Config: cfg, URL: rec.URL, Path: path, Secret: rec.Secret}
		return r.Unregister(ctx, hook, AppWebhookHook{ID: rec.HookID, Extra: rec.Extra})
	}()
	if err != nil {
		log.Error().Err(err).Msg("app webhook: could not remove the hook from the app; remove it there by hand")
	} else {
		log.Info().Msg("app webhook removed")
	}
	if err := tm.appDeps().store.delete(ctx, rec.WorkflowID, rec.NodeID); err != nil {
		log.Error().Err(err).Msg("app webhook: forgetting the stored hook")
	}
}
