package workflow

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// webhookTLSCertEnv/webhookTLSKeyEnv let an operator supply a real
// certificate/key pair for the webhook listener instead of the
// auto-generated self-signed one — see resolveTLSConfig.
const (
	webhookTLSCertEnv = "MONOAGENT_WEBHOOK_TLS_CERT"
	webhookTLSKeyEnv  = "MONOAGENT_WEBHOOK_TLS_KEY"

	// webhookTLSCacheDir (under ~/.monoagent) holds the cached self-signed
	// certificate; webhookCertCommonName is its subject.
	webhookTLSCacheDir    = "webhook-tls"
	webhookCertCommonName = "monoagentcli webhook server (self-signed)"
)

// WebhookRegistration holds the config for a registered webhook.
type WebhookRegistration struct {
	WorkflowID string
	NodeID     string
	Path       string             // unique URL path segment, e.g. UUID
	Method     string             // "GET", "POST", "ANY"
	HMACSecret string             // if non-empty, validate X-Hub-Signature-256 header
	AuthHeader string             // if non-empty, validate this header equals AuthToken
	AuthToken  string             // secret value expected in AuthHeader
	TriggerFn  func(items []Item) // called when the webhook fires (when the server has no runner)

	// MaxBodyBytes caps the request body; 0 means 1MB.
	MaxBodyBytes int64
	// ResponseMode is WebhookRespondOnReceived (""), WebhookRespondLastNode
	// or WebhookRespondNode. The waiting modes need the server's runner.
	ResponseMode string
	// ResponseCode is the status of an onReceived answer; 0 means 200.
	ResponseCode int
	// ResponseData picks what lastNode answers with: "firstEntryJson" (""),
	// "allEntries" or "noData".
	ResponseData string
	// ResponseTimeout bounds a waiting mode; 0 means 30s.
	ResponseTimeout time.Duration
	// DedupeHeader names the request header carrying a delivery ID; a
	// delivery whose ID was seen before starts no run.
	DedupeHeader string
	// VerifyToken, when set, must equal a hub.challenge check's
	// hub.verify_token (webhook_handshake.go).
	VerifyToken string
	// ChallengeJSON answers a hub.challenge check with JSON
	// {"hub.challenge": ...} rather than text (Strava).
	ChallengeJSON bool
	// AnswerGet answers a GET without a query 200 and starts no run: the
	// URL check of vendors that only probe the URL (Mailchimp).
	AnswerGet bool
	// Verify, when set, checks a delivery's signature (app webhooks whose
	// vendor signs them); an error answers 401 and starts no run.
	Verify func(header http.Header, body []byte) error
}

// WebhookServer is a standalone net/http server on a configurable port.
type WebhookServer struct {
	addr string
	// allowedOrigins is the CORS allowlist parsed once from
	// MONOAGENT_WEBHOOK_ALLOWED_ORIGINS. nil (unset/empty env) means NO CORS
	// headers are ever emitted — cross-origin browser callers are locked out,
	// which is correct for loopback/server-to-server webhook use.
	allowedOrigins map[string]struct{}
	mu             sync.RWMutex
	routes         map[string]*WebhookRegistration // path → registration
	server         *http.Server
	logger         zerolog.Logger
	// traceAdmit records a signed trace's crossing into a webhook run (the
	// daemon wires it to the org ledger). nil: signed traces are ignored and
	// every webhook run starts a fresh chain.
	traceAdmit TraceAdmitter
	// keyErrorOnce makes an unusable trace key an error in the log once,
	// rather than a warning on every request: without the key, loops
	// through webhooks are not counted.
	keyErrorOnce sync.Once
	// lockedLog spaces the log lines of requests refused for a locked
	// account (webhook_account.go).
	lockedLog webhookLockedLog
	// runner starts runs and reports their execution ID; execs reads them
	// back for the waiting response modes. nil runner: registrations'
	// TriggerFn is called and only onReceived is answered (webhook_run.go).
	runner WebhookRunner
	execs  WebhookExecutions
	// deliveries remembers delivery IDs for registrations with a
	// DedupeHeader; nil disables dedupe.
	deliveries WebhookDeliveries
	// replies serves /reply/ (wait_reply_http.go); nil answers 404.
	replies http.Handler
	// handshakes answers vendor URL checks (webhook_handshake.go).
	handshakes webhookHandshakes
	// forms are the trigger.form pages at /form/<path> (form_serve.go).
	forms formRoutes
	// chats are the trigger.chat pages at /chat/<path> (chat_serve.go).
	chats chatRoutes
	// watches is the chat watches' context (watchChatRun); Stop cancels it.
	watches    context.Context
	endWatches context.CancelFunc
}

// SetRunner makes the server start runs through run, which reports the
// execution ID (answered to the caller) or why no run started, and read them
// back through execs for the waiting response modes.
func (s *WebhookServer) SetRunner(run WebhookRunner, execs WebhookExecutions) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runner, s.execs = run, execs
}

// SetDeliveryStore installs the store of seen delivery IDs (dedupe_header).
func (s *WebhookServer) SetDeliveryStore(d WebhookDeliveries) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = d
}

// NewWebhookServer creates a server that will listen on addr (e.g. ":9321").
func NewWebhookServer(addr string, logger zerolog.Logger) *WebhookServer {
	s := &WebhookServer{
		addr:           addr,
		allowedOrigins: parseWebhookAllowedOrigins(os.Getenv("MONOAGENT_WEBHOOK_ALLOWED_ORIGINS")),
		routes:         make(map[string]*WebhookRegistration),
		logger:         logger,
	}
	s.watches, s.endWatches = context.WithCancel(context.Background())
	s.server = &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return s
}

// Start starts the HTTP server in a goroutine. Returns immediately.
//
// A loopback bind (the default, "127.0.0.1:9321") serves plain HTTP with no
// extra ceremony — same-user trust, mirroring the Chrome extension bridge.
// A non-loopback bind is only ever served over TLS: see resolveTLSConfig
// for the cert resolution order. Webhook payloads can carry caller-attached
// auth headers/tokens, so a non-loopback bind must never be served in the
// clear.
func (s *WebhookServer) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("webhook server: listen on %s: %w", s.addr, err)
	}

	tlsConfig, err := s.resolveTLSConfig()
	if err != nil {
		ln.Close()
		return err
	}

	scheme := "http"
	if tlsConfig != nil {
		ln = tls.NewListener(ln, tlsConfig)
		scheme = "https"
	}

	s.logger.Info().Str("addr", s.addr).Str("scheme", scheme).Msg("webhook server starting")
	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error().Err(err).Msg("webhook server error")
		}
	}()
	return nil
}

// Stop ends the chat watches and gracefully shuts down the server with a
// 5-second timeout.
func (s *WebhookServer) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s.endWatches()
	s.logger.Info().Msg("webhook server shutting down")
	return s.server.Shutdown(shutdownCtx)
}

// Register adds a webhook route. Path must be unique.
func (s *WebhookServer) Register(reg *WebhookRegistration) error {
	if reg == nil {
		return fmt.Errorf("webhook server: registration must not be nil")
	}
	if reg.Path == "" {
		return fmt.Errorf("webhook server: registration path must not be empty")
	}
	if reg.TriggerFn == nil {
		return fmt.Errorf("webhook server: registration TriggerFn must not be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.routes[reg.Path]; exists {
		return fmt.Errorf("webhook server: path %q is already registered", reg.Path)
	}
	s.routes[reg.Path] = reg
	s.logger.Info().
		Str("path", reg.Path).
		Str("workflow_id", reg.WorkflowID).
		Str("node_id", reg.NodeID).
		Msg("webhook registered")
	return nil
}

// Deregister removes a webhook route by path.
func (s *WebhookServer) Deregister(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.routes, path)
	s.logger.Info().Str("path", path).Msg("webhook deregistered")
}

// ServeHTTP handles all incoming webhook requests.
// Routes: POST/GET /webhook/{path}
// Returns 404 if path not found, 405 if method doesn't match; once the
// request is accepted, serveRun (webhook_run.go) starts the run and answers.
//
// A locked monoes.me account refuses every request first (refuseWhileLocked),
// before the body is read and before admitTrace writes to the org ledger.
func (s *WebhookServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.refuseWhileLocked(w, r) || s.serveReply(w, r) {
		return
	}
	if p, ok := strings.CutPrefix(r.URL.Path, "/form/"); ok {
		s.serveForm(w, r, p)
		return
	}
	if p, ok := strings.CutPrefix(r.URL.Path, "/chat/"); ok {
		s.serveChat(w, r, p)
		return
	}
	// Parse path: must be /webhook/{path}
	urlPath := r.URL.Path
	const prefix = "/webhook/"
	if !strings.HasPrefix(urlPath, prefix) {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	pathKey := strings.TrimPrefix(urlPath, prefix)
	pathKey = strings.Trim(pathKey, "/")
	if pathKey == "" {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}

	s.mu.RLock()
	reg, ok := s.routes[pathKey]
	s.mu.RUnlock()

	// CORS (hardened, RA2-8): emit NO CORS headers by default. Webhooks are
	// meant for loopback/server-to-server callers, so browsers cannot call
	// them cross-origin unless the operator configures an explicit origin
	// allowlist via MONOAGENT_WEBHOOK_ALLOWED_ORIGINS — in which case only
	// listed origins are reflected, never arbitrary ones.
	if origin := r.Header.Get("Origin"); origin != "" {
		if _, allowed := s.allowedOrigins[origin]; allowed {
			allowedHeaders := "Content-Type, X-Hub-Signature-256"
			if ok && reg.AuthHeader != "" {
				allowedHeaders += ", " + reg.AuthHeader
			}
			if ok && reg.DedupeHeader != "" {
				allowedHeaders += ", " + reg.DedupeHeader
			}
			// The response varies with the Origin header; without Vary a
			// shared cache could replay this at a disallowed origin.
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
		}
	}

	// Handle CORS preflight
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if !ok {
		writeJSONError(w, http.StatusNotFound, "webhook not found")
		return
	}

	// Vendor URL checks come before the method and auth checks: the vendor
	// proves the URL before it is configured with either.
	if s.answerHandshake(w, r, reg) {
		return
	}

	// Validate HTTP method
	if reg.Method != "ANY" {
		if r.Method != reg.Method {
			writeJSONError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method not allowed; expected %s", reg.Method))
			return
		}
	} else {
		// ANY: only allow GET and POST
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
	}

	maxBody := reg.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultWebhookMaxBody
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large or read error")
		return
	}
	if s.answerBodyHandshake(w, r, reg, bodyBytes) {
		return
	}

	// HMAC validation
	if reg.HMACSecret != "" {
		sigHeader := r.Header.Get("X-Hub-Signature-256")
		if sigHeader == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing X-Hub-Signature-256 header")
			return
		}
		if !validateHMAC(bodyBytes, reg.HMACSecret, sigHeader) {
			writeJSONError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
	}

	// Static header token validation
	if reg.AuthHeader != "" {
		got := r.Header.Get(reg.AuthHeader)
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(reg.AuthToken)) != 1 {
			writeJSONError(w, http.StatusUnauthorized, fmt.Sprintf("missing or invalid %s header", reg.AuthHeader))
			return
		}
	}

	s.serveRun(w, r, reg, pathKey, bodyBytes)
}
