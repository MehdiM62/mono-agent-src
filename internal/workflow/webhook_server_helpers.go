package workflow

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"syscall"

	"github.com/monoes/mono-agent/internal/tlsserve"
	"github.com/monoes/mono-agent/internal/tracesig"
)

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
