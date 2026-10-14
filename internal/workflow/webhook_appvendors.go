package workflow

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// Built-in app registrars, usable as trigger.webhook "register_with"
// values. More are declared the same way: an HTTPRegistrar and a
// RegisterWebhookRegistrar call (from the package that owns the node type).
func init() {
	RegisterWebhookRegistrar("typeform", TypeformRegistrar(""))
	RegisterWebhookRegistrar("calendly", CalendlyRegistrar(""))
}

// TypeformRegistrar registers a form's webhook (PUT
// /forms/{form_id}/webhooks/{tag}). Config: form_id, tag (default
// "monoagent-<path>"); credential: access_token, token or api_key.
// Deliveries are checked against Typeform-Signature. base "" is Typeform's
// API.
func TypeformRegistrar(base string) *HTTPRegistrar {
	if base == "" {
		base = "https://api.typeform.com"
	}
	hookURL := "/forms/{{config.form_id}}/webhooks/{{config.tag}}"
	return &HTTPRegistrar{
		BaseURL:  base,
		Auth:     "Bearer {{credential.access_token|credential.token|credential.api_key}}",
		Defaults: map[string]string{"tag": "monoagent-{{path}}"},
		Create: HTTPCall{Method: http.MethodPut, URL: hookURL, Body: map[string]interface{}{
			"url": "{{url}}", "enabled": true, "secret": "{{secret}}", "verify_ssl": true,
		}},
		IDPath:     "id",
		ExtraPaths: map[string]string{"tag": "tag"},
		Delete:     HTTPCall{Method: http.MethodDelete, URL: hookURL},
		Check:      &HTTPCall{Method: http.MethodGet, URL: hookURL},
		Verify:     verifyTypeform,
	}
}

// verifyTypeform checks Typeform-Signature: "sha256=" + base64 HMAC-SHA256.
func verifyTypeform(h http.Header, body []byte, secret string) error {
	sig, ok := strings.CutPrefix(h.Get("Typeform-Signature"), "sha256=")
	if !ok {
		return errors.New("missing Typeform-Signature")
	}
	got, err := base64.StdEncoding.DecodeString(sig)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	if err != nil || !hmac.Equal(mac.Sum(nil), got) {
		return errors.New("invalid Typeform-Signature")
	}
	return nil
}

// CalendlyRegistrar creates a webhook subscription (POST
// /webhook_subscriptions) and deletes it by its URI. Config: events (list),
// organization, scope (default "organization"), user (for scope "user");
// credential: access_token, token or api_key. Deliveries are checked against
// Calendly-Webhook-Signature. base "" is Calendly's API.
func CalendlyRegistrar(base string) *HTTPRegistrar {
	if base == "" {
		base = "https://api.calendly.com"
	}
	return &HTTPRegistrar{
		BaseURL:  base,
		Auth:     "Bearer {{credential.access_token|credential.token|credential.api_key}}",
		Defaults: map[string]string{"scope": "organization"},
		Create: HTTPCall{Method: http.MethodPost, URL: "/webhook_subscriptions", Body: map[string]interface{}{
			"url": "{{url}}", "events": "{{config.events}}", "organization": "{{config.organization}}",
			"user": "{{?config.user}}", "scope": "{{config.scope}}", "signing_key": "{{secret}}",
		}},
		IDPath: "resource.uri",
		Delete: HTTPCall{Method: http.MethodDelete, URL: "{{hook_id}}"},
		Check:  &HTTPCall{Method: http.MethodGet, URL: "{{hook_id}}"},
		Verify: verifyCalendly,
	}
}

// verifyCalendly checks Calendly-Webhook-Signature "t=<ts>,v1=<hex>", the
// HMAC-SHA256 of "<ts>.<body>".
func verifyCalendly(h http.Header, body []byte, secret string) error {
	var ts, v1 string
	for _, part := range strings.Split(h.Get("Calendly-Webhook-Signature"), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			v1 = v
		}
	}
	if ts == "" || v1 == "" {
		return errors.New("missing Calendly-Webhook-Signature")
	}
	got, err := hex.DecodeString(v1)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	if err != nil || !hmac.Equal(mac.Sum(nil), got) {
		return errors.New("invalid Calendly-Webhook-Signature")
	}
	return nil
}
