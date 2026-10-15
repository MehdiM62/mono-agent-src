package workflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Vendor URL checks. Many apps prove a webhook URL before they send events
// to it, each with its own handshake. The server answers them on every
// webhook route without any node config, and none of them starts a run:
//
//	HEAD                                   200, empty (Trello, SurveyMonkey)
//	GET  ?hub.mode=subscribe&hub.challenge  200 with the challenge as text
//	                                       (Meta Graph: Facebook, WhatsApp,
//	                                       Instagram; WebSub; Strava). When
//	                                       the node sets verify_token,
//	                                       hub.verify_token must equal it.
//	                                       challenge_format "json" answers
//	                                       {"hub.challenge": ...} instead
//	                                       (Strava).
//	GET  ?check=...  (its only parameter)  200 with the value as text (Onfleet)
//	GET  without a query, when the node    200, empty (Mailchimp)
//	     sets answer_get
//	?validationToken=...                   200 text/plain token (Microsoft Graph)
//	X-Hook-Secret header                   echoed back with 200 and kept as the
//	                                       node's static data "x_hook_secret"
//	                                       (Asana, Keap). A later request
//	                                       carrying X-Hook-Signature must be
//	                                       its HMAC-SHA256 of the body.
//	JSON {"type":"url_verification",       200 {"challenge": ...} (Slack)
//	      "challenge":...}
//	x-amz-sns-message-type:                the SubscribeURL is visited (only an
//	SubscriptionConfirmation               https URL on amazonaws.com), 200
//
// Each is answered only when the request has that vendor's exact shape, so
// an ordinary delivery is never mistaken for one.

// hookSecretKey is the static-data key the X-Hook-Secret handshake stores
// the secret under (node scope).
const hookSecretKey = "x_hook_secret"

// snsConfirmTimeout bounds the visit to an SNS SubscribeURL.
const snsConfirmTimeout = 10 * time.Second

// webhookHandshakes is the server's state for the vendor checks.
type webhookHandshakes struct {
	mu sync.Mutex
	// db keeps X-Hook-Secret values in workflow_static_data; nil keeps
	// them in mem (a server without an engine).
	db  *sql.DB
	mem map[string]string
	// client visits SNS SubscribeURLs; allowSNSHost decides which hosts it
	// may visit. Both are replaced in tests.
	client       *http.Client
	allowSNSHost func(host string) bool
}

// SetStateDB keeps handshake state (the X-Hook-Secret value) in db.
func (s *WebhookServer) SetStateDB(db *sql.DB) {
	s.handshakes.mu.Lock()
	defer s.handshakes.mu.Unlock()
	s.handshakes.db = db
}

// isSNSHost reports whether host is an AWS endpoint an SNS SubscribeURL may
// point at.
func isSNSHost(host string) bool {
	return strings.HasSuffix(strings.ToLower(host), ".amazonaws.com")
}

// answerHandshake answers the checks that need no body. It reports whether
// the request was one (and is answered).
func (s *WebhookServer) answerHandshake(w http.ResponseWriter, r *http.Request, reg *WebhookRegistration) bool {
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodHead:
		w.WriteHeader(http.StatusOK)
		return true
	case r.Method == http.MethodGet && q.Get("hub.mode") == "subscribe" && q.Has("hub.challenge"):
		if reg.VerifyToken != "" &&
			subtle.ConstantTimeCompare([]byte(q.Get("hub.verify_token")), []byte(reg.VerifyToken)) != 1 {
			writeJSONError(w, http.StatusForbidden, "hub.verify_token does not match")
			return true
		}
		if reg.ChallengeJSON {
			writeWebhookJSON(w, http.StatusOK, map[string]string{"hub.challenge": q.Get("hub.challenge")})
			return true
		}
		writeHandshakeText(w, q.Get("hub.challenge"))
		return true
	case r.Method == http.MethodGet && len(q) == 1 && q.Has("check"):
		writeHandshakeText(w, q.Get("check"))
		return true
	case r.Method == http.MethodGet && reg.AnswerGet && r.URL.RawQuery == "":
		w.WriteHeader(http.StatusOK)
		return true
	case (r.Method == http.MethodGet || r.Method == http.MethodPost) && q.Get("validationToken") != "":
		writeHandshakeText(w, q.Get("validationToken"))
		return true
	case r.Header.Get("X-Hook-Secret") != "":
		secret := r.Header.Get("X-Hook-Secret")
		if err := s.setHookSecret(r.Context(), reg, secret); err != nil {
			s.logger.Error().Err(err).Str("workflow_id", reg.WorkflowID).Msg("webhook: storing X-Hook-Secret")
			writeJSONError(w, http.StatusServiceUnavailable, "could not store the hook secret; try again")
			return true
		}
		w.Header().Set("X-Hook-Secret", secret)
		w.WriteHeader(http.StatusOK)
		return true
	}
	return false
}

// answerBodyHandshake answers the checks recognised by their body (Slack,
// SNS), and refuses a request whose X-Hook-Signature does not match the
// stored X-Hook-Secret. It reports whether the request is answered.
func (s *WebhookServer) answerBodyHandshake(w http.ResponseWriter, r *http.Request, reg *WebhookRegistration, body []byte) bool {
	if sig := r.Header.Get("X-Hook-Signature"); sig != "" {
		secret, err := s.hookSecret(r.Context(), reg)
		if err != nil {
			s.logger.Error().Err(err).Str("workflow_id", reg.WorkflowID).Msg("webhook: reading X-Hook-Secret")
			writeJSONError(w, http.StatusServiceUnavailable, "could not read the hook secret; try again")
			return true
		}
		if secret != "" && !validHookSignature(body, secret, sig) {
			writeJSONError(w, http.StatusUnauthorized, "invalid X-Hook-Signature")
			return true
		}
	}

	if r.Header.Get("X-Amz-Sns-Message-Type") == "SubscriptionConfirmation" {
		s.confirmSNS(w, r, body)
		return true
	}

	if !strings.Contains(r.Header.Get("Content-Type"), "json") {
		return false
	}
	var slack struct {
		Type      string  `json:"type"`
		Challenge *string `json:"challenge"`
	}
	if json.Unmarshal(body, &slack) != nil || slack.Type != "url_verification" || slack.Challenge == nil {
		return false
	}
	writeWebhookJSON(w, http.StatusOK, map[string]string{"challenge": *slack.Challenge})
	return true
}

// confirmSNS visits an SNS SubscriptionConfirmation's SubscribeURL.
func (s *WebhookServer) confirmSNS(w http.ResponseWriter, r *http.Request, body []byte) {
	var msg struct {
		SubscribeURL string `json:"SubscribeURL"`
	}
	if err := json.Unmarshal(body, &msg); err != nil || msg.SubscribeURL == "" {
		writeJSONError(w, http.StatusBadRequest, "SNS SubscriptionConfirmation without a SubscribeURL")
		return
	}
	s.handshakes.mu.Lock()
	client, allow := s.handshakes.client, s.handshakes.allowSNSHost
	s.handshakes.mu.Unlock()
	if client == nil {
		client = &http.Client{Timeout: snsConfirmTimeout}
	}
	if allow == nil {
		allow = isSNSHost
	}
	u, err := url.Parse(msg.SubscribeURL)
	if err != nil || u.Scheme != "https" || !allow(u.Hostname()) {
		writeJSONError(w, http.StatusBadRequest, "SNS SubscribeURL must be an https URL on amazonaws.com")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snsConfirmTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "SNS SubscribeURL is not usable")
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "could not confirm the SNS subscription: "+err.Error())
		return
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("SNS answered the confirmation with %d", resp.StatusCode))
		return
	}
	writeWebhookJSON(w, http.StatusOK, map[string]interface{}{"success": true, "confirmed": true})
}

// writeHandshakeText answers a challenge as plain text, never sniffed as
// HTML: the value comes from the caller.
func writeHandshakeText(w http.ResponseWriter, v string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(v))
}

// validHookSignature checks an X-Hook-Signature: the hex HMAC-SHA256 of
// the body keyed with the X-Hook-Secret.
func validHookSignature(body []byte, secret, sig string) bool {
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), got)
}

func (s *WebhookServer) setHookSecret(ctx context.Context, reg *WebhookRegistration, v string) error {
	h := &s.handshakes
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db != nil {
		sd := &StaticData{db: h.db, workflowID: reg.WorkflowID, nodeID: reg.NodeID}
		return sd.Set(ctx, hookSecretKey, v)
	}
	if h.mem == nil {
		h.mem = make(map[string]string)
	}
	h.mem[reg.WorkflowID+"\x00"+reg.NodeID] = v
	return nil
}

func (s *WebhookServer) hookSecret(ctx context.Context, reg *WebhookRegistration) (string, error) {
	h := &s.handshakes
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db != nil {
		var v string
		sd := &StaticData{db: h.db, workflowID: reg.WorkflowID, nodeID: reg.NodeID}
		if _, err := sd.Get(ctx, hookSecretKey, &v); err != nil {
			return "", err
		}
		return v, nil
	}
	return h.mem[reg.WorkflowID+"\x00"+reg.NodeID], nil
}
