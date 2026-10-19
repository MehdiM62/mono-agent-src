package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/credhttp"
)

// appHookHTTP sends an app webhook's registration requests when the hook
// names no client: they carry the connection's token, so a redirect
// elsewhere gets none of it (credhttp).
var appHookHTTP = credhttp.Client(&http.Client{Timeout: 20 * time.Second})

// HTTPRegistrar is a WebhookRegistrar declared as data: the API calls that
// create, check and delete a vendor's webhook, written as templates.
//
// A template holds {{name}} placeholders:
//
//	url, secret, path          the AppWebhook's public URL, secret and path
//	hook_id                    the stored hook's id (check, delete)
//	workflow_id, node_id       the node
//	config.<key>               a node config value (after Defaults)
//	credential.<key>           a value of the node's credential
//	extra.<key>                a value kept from the create response
//	expires_at                 now + Lifetime, RFC 3339 UTC (create, renew)
//
// "{{a|b}}" takes the first that is set. A missing value is an error, except
// in a body string that is exactly "{{?name}}", whose key is then left out.
// A body string that is exactly one placeholder keeps the value's JSON type
// (an events list stays a list). In a call's URL the values are path-escaped
// and a relative URL is joined to BaseURL; a URL that is exactly one
// placeholder (e.g. "{{hook_id}}" when the vendor's id is the hook's URL) is
// used as it is, and must be on BaseURL's host, so a credential is never
// sent elsewhere.
type HTTPRegistrar struct {
	// BaseURL is the vendor API, e.g. "https://api.typeform.com".
	BaseURL string
	// AuthHeader (default "Authorization") is sent with the expanded Auth,
	// e.g. "Bearer {{credential.access_token|credential.api_key}}".
	AuthHeader string
	Auth       string
	// Defaults fill config keys that are missing or empty (templates).
	Defaults map[string]string
	// Create registers the hook; IDPath (dotted, e.g. "resource.uri") picks
	// the hook id from its JSON response and ExtraPaths more values to keep.
	Create     HTTPCall
	IDPath     string
	ExtraPaths map[string]string
	// Delete removes the hook; a 404 counts as removed.
	Delete HTTPCall
	// Check, when set, answers whether the hook exists (2xx yes, 404 no).
	Check *HTTPCall
	// Lifetime, when set, is how long a hook lives after create or renew:
	// Register reports now + Lifetime as its expiry, or the RFC 3339 time
	// at ExpiresPath in the answer when that is set, and the daemon renews
	// it with RenewCall before then (WebhookRenewer).
	Lifetime    time.Duration
	ExpiresPath string
	// RenewCall, when set, extends the hook (e.g. Microsoft Graph PATCH
	// /subscriptions/{{hook_id}} with {"expirationDateTime": "{{expires_at}}"}).
	RenewCall *HTTPCall
	// Verify, when set, checks a delivery with the registration's secret.
	Verify func(header http.Header, body []byte, secret string) error
	// Client defaults to a 20-second-timeout client.
	Client *http.Client
}

// HTTPCall is one API call of an HTTPRegistrar.
type HTTPCall struct {
	Method string
	URL    string
	Body   map[string]interface{}
}

var placeholderRe = regexp.MustCompile(`\{\{\s*(\??)([^{}]+?)\s*\}\}`)

// templateVars resolves placeholder names for one call.
type templateVars struct {
	hook    AppWebhook
	stored  AppWebhookHook
	expires string // expires_at
}

func (v templateVars) lookup(name string) (interface{}, bool) {
	for _, alt := range strings.Split(name, "|") {
		val := v.lookupOne(strings.TrimSpace(alt))
		if val != nil && val != "" {
			return val, true
		}
	}
	return nil, false
}

func (v templateVars) lookupOne(name string) interface{} {
	switch name {
	case "url":
		return v.hook.URL
	case "secret":
		return v.hook.Secret
	case "path":
		return v.hook.Path
	case "hook_id":
		return v.stored.ID
	case "workflow_id":
		return v.hook.WorkflowID
	case "node_id":
		return v.hook.NodeID
	case "expires_at":
		return v.expires
	}
	scope, key, ok := strings.Cut(name, ".")
	if !ok {
		return nil
	}
	switch scope {
	case "config":
		return v.hook.Config[key]
	case "credential":
		cred, _ := v.hook.Config["credential"].(map[string]interface{})
		return cred[key]
	case "extra":
		return v.stored.Extra[key]
	}
	return nil
}

func templateString(val interface{}) string {
	switch t := val.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// expand replaces every placeholder of s, passing values through esc.
func (v templateVars) expand(s string, esc func(string) string) (string, error) {
	var missing []string
	out := placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		name := placeholderRe.FindStringSubmatch(m)[2]
		val, ok := v.lookup(name)
		if !ok {
			missing = append(missing, name)
			return ""
		}
		return esc(templateString(val))
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%s is not set", strings.Join(missing, ", "))
	}
	return out, nil
}

// expandBody expands a body template, keeping single-placeholder values' types.
func (v templateVars) expandBody(in interface{}) (interface{}, bool, error) {
	switch t := in.(type) {
	case string:
		if m := placeholderRe.FindStringSubmatch(t); m != nil && m[0] == t {
			val, ok := v.lookup(m[2])
			if !ok && m[1] == "?" {
				return nil, false, nil
			}
			if !ok {
				return nil, false, fmt.Errorf("%s is not set", m[2])
			}
			return val, true, nil
		}
		s, err := v.expand(t, func(s string) string { return s })
		return s, true, err
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, item := range t {
			val, keep, err := v.expandBody(item)
			if err != nil {
				return nil, false, err
			}
			if keep {
				out[k] = val
			}
		}
		return out, true, nil
	case []interface{}:
		out := make([]interface{}, 0, len(t))
		for _, item := range t {
			val, keep, err := v.expandBody(item)
			if err != nil {
				return nil, false, err
			}
			if keep {
				out = append(out, val)
			}
		}
		return out, true, nil
	}
	return in, true, nil
}

// withDefaults returns hook with Defaults applied to its config.
func (h *HTTPRegistrar) withDefaults(hook AppWebhook) (AppWebhook, error) {
	cfg := make(map[string]interface{}, len(hook.Config)+len(h.Defaults))
	for k, v := range hook.Config {
		cfg[k] = v
	}
	hook.Config = cfg
	for k, tmpl := range h.Defaults {
		if v, ok := cfg[k]; ok && v != nil && v != "" {
			continue
		}
		val, err := templateVars{hook: hook}.expand(tmpl, func(s string) string { return s })
		if err != nil {
			return hook, fmt.Errorf("default for %s: %w", k, err)
		}
		cfg[k] = val
	}
	return hook, nil
}

// do makes one call and returns the status and response body.
func (h *HTTPRegistrar) do(ctx context.Context, call HTTPCall, hook AppWebhook, stored AppWebhookHook) (int, []byte, error) {
	hook, err := h.withDefaults(hook)
	if err != nil {
		return 0, nil, err
	}
	vars := templateVars{hook: hook, stored: stored}
	if h.Lifetime > 0 {
		vars.expires = h.expiry().Format(time.RFC3339)
	}
	target, err := h.callURL(vars, call.URL)
	if err != nil {
		return 0, nil, err
	}
	var body io.Reader
	if call.Body != nil {
		b, _, err := vars.expandBody(call.Body)
		if err != nil {
			return 0, nil, err
		}
		raw, _ := json.Marshal(b)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, call.Method, target, body)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if h.Auth != "" {
		auth, err := vars.expand(h.Auth, func(s string) string { return s })
		if err != nil {
			return 0, nil, fmt.Errorf("auth: %w", err)
		}
		name := h.AuthHeader
		if name == "" {
			name = "Authorization"
		}
		req.Header.Set(name, auth)
	}
	client := h.Client
	if client == nil {
		client = appHookHTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, nil
}

// callURL expands a call's URL template (see HTTPRegistrar).
func (h *HTTPRegistrar) callURL(vars templateVars, tmpl string) (string, error) {
	if m := placeholderRe.FindStringSubmatch(tmpl); m != nil && m[0] == tmpl {
		raw, err := vars.expand(tmpl, func(s string) string { return s })
		if err != nil {
			return "", err
		}
		u, err := url.Parse(raw)
		base, berr := url.Parse(h.BaseURL)
		if err != nil || berr != nil || !u.IsAbs() || u.Host != base.Host {
			return "", fmt.Errorf("hook URL %q is not on %s", raw, h.BaseURL)
		}
		return raw, nil
	}
	s, err := vars.expand(tmpl, url.PathEscape)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(s, "/") {
		s = strings.TrimRight(h.BaseURL, "/") + s
	}
	return s, nil
}

func callError(what string, status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return fmt.Errorf("%s: the app answered HTTP %d: %s", what, status, msg)
}

// Register implements WebhookRegistrar.
func (h *HTTPRegistrar) Register(ctx context.Context, hook AppWebhook) (AppWebhookHook, error) {
	status, body, err := h.do(ctx, h.Create, hook, AppWebhookHook{})
	if err != nil {
		return AppWebhookHook{}, fmt.Errorf("create: %w", err)
	}
	if status < 200 || status > 299 {
		return AppWebhookHook{}, callError("create", status, body)
	}
	var doc interface{}
	if err := json.Unmarshal(body, &doc); err != nil && h.IDPath != "" {
		return AppWebhookHook{}, fmt.Errorf("create: the answer is not JSON: %w", err)
	}
	got := AppWebhookHook{Extra: map[string]interface{}{}}
	if h.IDPath != "" {
		id := jsonPath(doc, h.IDPath)
		if id == nil || id == "" {
			return AppWebhookHook{}, fmt.Errorf("create: the answer has no %s", h.IDPath)
		}
		got.ID = templateString(id)
	}
	for k, p := range h.ExtraPaths {
		if v := jsonPath(doc, p); v != nil {
			got.Extra[k] = v
		}
	}
	if h.Lifetime > 0 {
		if got.ExpiresAt, err = h.answeredExpiry(doc); err != nil {
			return AppWebhookHook{}, fmt.Errorf("create: %w", err)
		}
	}
	return got, nil
}

// expiry is the expiry a create or renew made now asks for.
func (h *HTTPRegistrar) expiry() time.Time {
	return time.Now().UTC().Add(h.Lifetime).Truncate(time.Second)
}

// expiryLayouts are the time formats an ExpiresPath value may have: RFC
// 3339 (Graph) and Jira's "2026-11-13T09:00:00.000+0000".
var expiryLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05-0700"}

// answeredExpiry reads the expiry from a create or renew answer: the time
// at ExpiresPath, or now + Lifetime without one.
func (h *HTTPRegistrar) answeredExpiry(doc interface{}) (time.Time, error) {
	if h.ExpiresPath == "" {
		return h.expiry(), nil
	}
	raw, _ := jsonPath(doc, h.ExpiresPath).(string)
	for _, layout := range expiryLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("the answer has no expiry time at %s (got %q)", h.ExpiresPath, raw)
}

// Renew implements WebhookRenewer: it sends RenewCall and returns the new
// expiry. A registrar without RenewCall cannot extend its hooks.
func (h *HTTPRegistrar) Renew(ctx context.Context, hook AppWebhook, stored AppWebhookHook) (time.Time, error) {
	if h.RenewCall == nil || h.Lifetime <= 0 {
		return time.Time{}, errors.New("renew: this app registrar has no renew call")
	}
	status, body, err := h.do(ctx, *h.RenewCall, hook, stored)
	if err != nil {
		return time.Time{}, fmt.Errorf("renew: %w", err)
	}
	if status < 200 || status > 299 {
		return time.Time{}, callError("renew", status, body)
	}
	var doc interface{}
	_ = json.Unmarshal(body, &doc)
	exp, err := h.answeredExpiry(doc)
	if err != nil {
		return time.Time{}, fmt.Errorf("renew: %w", err)
	}
	return exp, nil
}

// Unregister implements WebhookRegistrar.
func (h *HTTPRegistrar) Unregister(ctx context.Context, hook AppWebhook, stored AppWebhookHook) error {
	status, body, err := h.do(ctx, h.Delete, hook, stored)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if status == http.StatusNotFound || (status >= 200 && status <= 299) {
		return nil
	}
	return callError("delete", status, body)
}

// Exists implements WebhookChecker (only meaningful with Check set).
func (h *HTTPRegistrar) Exists(ctx context.Context, hook AppWebhook, stored AppWebhookHook) (bool, error) {
	if h.Check == nil {
		return true, nil
	}
	status, body, err := h.do(ctx, *h.Check, hook, stored)
	if err != nil {
		return false, fmt.Errorf("check: %w", err)
	}
	switch {
	case status == http.StatusNotFound:
		return false, nil
	case status >= 200 && status <= 299:
		return true, nil
	}
	return false, callError("check", status, body)
}

// VerifyDelivery implements WebhookVerifier.
func (h *HTTPRegistrar) VerifyDelivery(header http.Header, body []byte, hook AppWebhook, _ AppWebhookHook) error {
	if h.Verify == nil || hook.Secret == "" {
		return nil
	}
	return h.Verify(header, body, hook.Secret)
}

// jsonPath walks a dotted path through decoded JSON objects; a number
// part indexes a list ("webhookRegistrationResult.0.createdWebhookId").
func jsonPath(doc interface{}, path string) interface{} {
	cur := doc
	for _, part := range strings.Split(path, ".") {
		switch t := cur.(type) {
		case map[string]interface{}:
			cur = t[part]
		case []interface{}:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(t) {
				return nil
			}
			cur = t[i]
		default:
			return nil
		}
	}
	return cur
}
