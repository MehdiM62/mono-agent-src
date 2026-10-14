package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Renewal of app webhooks that expire. A registrar implementing
// WebhookRenewer returns the hook's expiry from Register; the daemon checks
// its active app hooks every appHookRenewInterval and renews each one once
// appHookRenewAt of its lifetime has passed (75%: a Graph subscription of
// three days is renewed after about 54 hours). A failed renewal is logged
// with its error class, kept on the app_webhooks row and tried again with
// backoff (appHookRenewBackoff) until it succeeds or the hook is gone.

var (
	// appHookRenewInterval is how often the daemon looks for hooks due.
	appHookRenewInterval = time.Minute
	// appHookRenewBackoff is the first retry delay after a failed renewal,
	// doubled per failure up to appHookRenewMaxBackoff.
	appHookRenewBackoff    = time.Minute
	appHookRenewMaxBackoff = 30 * time.Minute
)

// appHookRenewAt is the share of a hook's lifetime after which it is renewed.
const appHookRenewAt = 0.75

// appRenewFailures holds the retry state of hooks whose renewal failed,
// keyed by appWebhookKey.
type appRenewFailures struct {
	mu sync.Mutex
	m  map[string]appRenewFailure
}

type appRenewFailure struct {
	hookID string
	n      int
	next   time.Time
}

// renewDue reports whether rec's hook should be renewed at now.
func renewDue(rec *appWebhookRecord, now time.Time) bool {
	if rec.ExpiresAt.IsZero() || rec.HookID == "" {
		return false
	}
	since := rec.RenewedAt
	if since.IsZero() || since.After(rec.ExpiresAt) {
		return true
	}
	life := rec.ExpiresAt.Sub(since)
	return !now.Before(since.Add(time.Duration(float64(life) * appHookRenewAt)))
}

// startAppHookRenewal runs renewAppHooks every appHookRenewInterval for
// ctx's lifetime.
func (tm *TriggerManager) startAppHookRenewal(ctx context.Context) {
	go func() {
		t := time.NewTicker(appHookRenewInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				tm.renewAppHooks(ctx, now)
			}
		}
	}()
}

// renewAppHooks renews the due hooks of this manager's active app webhook
// triggers. Hooks without an active trigger here are left alone: they
// belong to no running workflow.
func (tm *TriggerManager) renewAppHooks(ctx context.Context, now time.Time) {
	tm.mu.Lock()
	var states []appHookState
	for _, entries := range tm.active {
		for _, e := range entries {
			if e.app != nil {
				states = append(states, *e.app)
			}
		}
	}
	tm.mu.Unlock()
	for i := range states {
		tm.renewAppHook(ctx, &states[i], now)
	}
}

func (tm *TriggerManager) renewAppHook(ctx context.Context, st *appHookState, now time.Time) {
	r, ok := WebhookRegistrarFor(st.name).(WebhookRenewer)
	if !ok {
		return
	}
	store := tm.appDeps().store
	if store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, appHookTimeout)
	defer cancel()
	rec, err := store.get(ctx, st.workflowID, st.nodeID)
	if err != nil || rec == nil || !renewDue(rec, now) {
		return
	}
	key := appWebhookKey(rec.WorkflowID, rec.NodeID)
	tm.renewFails.mu.Lock()
	f, failed := tm.renewFails.m[key]
	tm.renewFails.mu.Unlock()
	if failed && f.hookID == rec.HookID && now.Before(f.next) {
		return
	}

	log := tm.logger.With().Str("workflow_id", rec.WorkflowID).Str("node_id", rec.NodeID).
		Str("registrar", rec.Registrar).Str("hook_id", rec.HookID).Logger()
	expires, err := func() (time.Time, error) {
		cfg, err := tm.withCredential(ctx, st.profileID, rec.Config)
		if err != nil {
			return time.Time{}, err
		}
		path := rec.URL[strings.LastIndex(rec.URL, "/webhook/")+len("/webhook/"):]
		hook := AppWebhook{WorkflowID: rec.WorkflowID, NodeID: rec.NodeID, Config: cfg, URL: rec.URL, Path: path, Secret: rec.Secret}
		exp, err := r.Renew(ctx, hook, AppWebhookHook{ID: rec.HookID, Extra: rec.Extra, ExpiresAt: rec.ExpiresAt})
		if err == nil && !exp.After(now) {
			err = fmt.Errorf("the app gave the renewed hook an expiry in the past (%s)", exp.Format(time.RFC3339))
		}
		return exp, err
	}()

	if err != nil {
		class := ClassifyError(err)
		if !failed || f.hookID != rec.HookID {
			f = appRenewFailure{hookID: rec.HookID}
		}
		f.n++
		delay := min(appHookRenewBackoff<<min(f.n-1, 16), appHookRenewMaxBackoff)
		f.next = now.Add(delay)
		tm.renewFails.mu.Lock()
		if tm.renewFails.m == nil {
			tm.renewFails.m = make(map[string]appRenewFailure)
		}
		tm.renewFails.m[key] = f
		tm.renewFails.mu.Unlock()
		log.Error().Err(err).Str("error_class", string(class)).Int("attempt", f.n).Time("expires_at", rec.ExpiresAt).
			Time("next_attempt", f.next).Msg("app webhook: renewing the hook failed")
		if serr := store.renewed(ctx, rec.WorkflowID, rec.NodeID, rec.HookID, time.Time{}, time.Time{}, err.Error(), class); serr != nil {
			log.Error().Err(serr).Msg("app webhook: recording the failed renewal")
		}
		return
	}
	tm.renewFails.mu.Lock()
	delete(tm.renewFails.m, key)
	tm.renewFails.mu.Unlock()
	if err := store.renewed(ctx, rec.WorkflowID, rec.NodeID, rec.HookID, expires, now, "", ""); err != nil {
		log.Error().Err(err).Msg("app webhook: recording the renewal")
		return
	}
	log.Info().Time("expires_at", expires).Msg("app webhook renewed")
}
