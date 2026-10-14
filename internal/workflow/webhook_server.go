package workflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/tlsserve"
	"github.com/monoes/mono-agent/internal/tracesig"
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
	// handshakes answers vendor URL checks (webhook_handshake.go).
	handshakes webhookHandshakes
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

// TraceAdmitter records that a request carrying a verified trace on chain
// at hop is starting workflowID, and decides whether it may: it returns the
// hop the run is at, or a refusal (e.g. the chain reached its hop limit).
type TraceAdmitter func(ctx context.Context, workflowID, chain string, hop int) (admittedHop int, refusal string, err error)

// SetTraceAdmitter installs the admitter for signed traces (see
// TraceAdmitter).
func (s *WebhookServer) SetTraceAdmitter(fn TraceAdmitter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traceAdmit = fn
}

// admitTrace works out the chain a webhook run is on. A verified
// X-Monoagent-Trace header continues its chain, recorded through the
// admitter; anything else (no header, one that does not verify) starts a
// fresh chain at hop 0. Every webhook run is on a chain, so a workflow that
// calls its own webhook, or another that calls back, is a loop the ledger
// sees from its second request on, even when nothing started it from an
// org. A non-zero status is a refusal to send back: 429 when the ledger
// refuses (hop limit, or orgbridge.TriggerRepeats runs a minute), 503 when
// it cannot record. An expired token does not verify. Without an admitter (no
// daemon ledger) no chain is given, since nothing would count it.
func (s *WebhookServer) admitTrace(ctx context.Context, workflowID, token string) (trace map[string]interface{}, status int, msg string) {
	s.mu.RLock()
	admit := s.traceAdmit
	s.mu.RUnlock()
	if admit == nil {
		return nil, 0, ""
	}
	fresh := map[string]interface{}{"chain_id": tracesig.NewChainID(), "hop": 0}
	if token == "" {
		return fresh, 0, ""
	}
	key, err := tracesig.Key()
	if err != nil {
		s.keyErrorOnce.Do(func() {
			s.logger.Error().Err(err).Msg("webhook: the trace key is unusable, so loops through webhooks are not counted until it is fixed (see ~/.monoagent/trace.key)")
		})
		return fresh, 0, ""
	}
	chain, hop, ok := tracesig.Verify(key, token)
	if !ok {
		// Not signed here (or by an older key): an outside caller cannot
		// choose the chain, so the run starts a fresh one.
		s.logger.Debug().Str("workflow_id", workflowID).Msg("webhook: unverified trace header ignored")
		return fresh, 0, ""
	}
	admitted, refusal, err := admit(ctx, workflowID, chain, hop)
	if err != nil {
		// Fail closed: continuing without the ledger would let a loop
		// through unrecorded.
		s.logger.Error().Err(err).Str("workflow_id", workflowID).Msg("webhook: could not record the trace crossing")
		return nil, http.StatusServiceUnavailable, "could not record this request's chain; try again"
	}
	if refusal != "" {
		return nil, http.StatusTooManyRequests, refusal
	}
	return map[string]interface{}{"chain_id": chain, "hop": admitted}, 0, ""
}

// NewWebhookServer creates a server that will listen on addr (e.g. ":9321").
func NewWebhookServer(addr string, logger zerolog.Logger) *WebhookServer {
	s := &WebhookServer{
		addr:           addr,
		allowedOrigins: parseWebhookAllowedOrigins(os.Getenv("MONOAGENT_WEBHOOK_ALLOWED_ORIGINS")),
		routes:         make(map[string]*WebhookRegistration),
		logger:         logger,
	}
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

// resolveTLSConfig decides whether the webhook listener serves TLS and, if
// so, builds the *tls.Config. The rules are shared with the OpenAI-compatible
// API (internal/tlsserve): a loopback bind is plain HTTP, and any other bind
// is only ever served over TLS, never plaintext, because webhook payloads can
// carry caller-attached auth headers and tokens.
func (s *WebhookServer) resolveTLSConfig() (*tls.Config, error) {
	return tlsserve.Resolve(tlsserve.Config{
		Addr:       s.addr,
		CertEnv:    webhookTLSCertEnv,
		KeyEnv:     webhookTLSKeyEnv,
		CacheDir:   webhookTLSCacheDir,
		CommonName: webhookCertCommonName,
		Label:      "webhook server",
		Warn:       func(msg string) { s.logger.Warn().Str("addr", s.addr).Msg(msg) },
	})
}

// isLoopbackAddr reports whether addr (a "host:port" bind address) resolves
// to loopback only.
func isLoopbackAddr(addr string) bool { return tlsserve.IsLoopbackAddr(addr) }

// generateSelfSignedCert creates the webhook server's self-signed
// certificate (localhost, 127.0.0.1 and ::1 only).
func generateSelfSignedCert() (cert tls.Certificate, certPEM, keyPEM []byte, err error) {
	return tlsserve.GenerateSelfSigned(webhookCertCommonName)
}

// isAddrInUse reports whether err is a "port already taken" listen failure.
// syscall.EADDRINUSE covers unix; Windows reports WSAEADDRINUSE, which does not
// compare equal to it, so its message is matched as well.
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address")
}

// Stop gracefully shuts down the server with a 5-second timeout.
func (s *WebhookServer) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
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
	if s.refuseWhileLocked(w, r) {
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

// parseWebhookAllowedOrigins parses MONOAGENT_WEBHOOK_ALLOWED_ORIGINS — a
// comma-separated list of origins (e.g. "https://app.example.com") that may
// call the webhooks from a browser. Empty/unset (or all-empty entries)
// yields nil: no CORS headers at all.
func parseWebhookAllowedOrigins(v string) map[string]struct{} {
	var out map[string]struct{}
	for _, o := range strings.Split(v, ",") {
		if o = strings.TrimSpace(o); o != "" {
			if out == nil {
				out = make(map[string]struct{})
			}
			out[o] = struct{}{}
		}
	}
	return out
}

// validateHMAC checks the X-Hub-Signature-256 header against the body and secret.
// The header format is: "sha256=<hex digest>"
func validateHMAC(body []byte, secret, sigHeader string) bool {
	const sigPrefix = "sha256="
	if !strings.HasPrefix(sigHeader, sigPrefix) {
		return false
	}
	sigHex := strings.TrimPrefix(sigHeader, sigPrefix)
	sigBytes, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)

	return hmac.Equal(expected, sigBytes)
}

// writeJSONError writes a JSON error response with the given status code and message.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	resp, _ := json.Marshal(map[string]string{"error": msg})
	_, _ = w.Write(resp)
}
