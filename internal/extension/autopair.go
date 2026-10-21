package extension

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Zero-step pairing.
//
// A freshly installed extension has no token, and the only way the old design
// let it learn that the person had signed in was a paired socket. So every new
// browser showed "Sign in" and "run extension pair" even on a machine where
// the app was signed in already. The claim flow removes the step: when the
// bridge is up and the account is signed in, the extension asks
// GET /monoagent/status, then POST /monoagent/pair/claim, and receives a
// token of its own.
//
// Who may claim (all must hold, otherwise 403 with nothing handed out):
//   - the TCP peer is loopback and the Host header is a loopback name (a
//     DNS-rebound web page carries its own domain as Host);
//   - the Origin header is chrome-extension:// or moz-extension:// with a host.
//     A web page always carries its http(s) origin; a request with no Origin
//     (curl, a local script) is refused too, so the flow is not a free token
//     for scripts that never needed one: they can read extension.token;
//   - Sec-Fetch-Site, where sent, is not same-origin/same-site, and
//     Sec-Fetch-Mode is not navigate;
//   - when MONOAGENT_EXTENSION_IDS lists extension ids, the Origin is one of them;
//   - auto-pairing is on (`extension autopair off`, MONOAGENT_EXTENSION_AUTOPAIR=0);
//   - the monoes.me account is signed in (the guard every gated command uses);
//   - fewer than claimsPerWindow claims were made in the last claimWindow.
//
// What is NOT defended: another extension that holds host permission for
// 127.0.0.1 can send an extension Origin of its own (and a same-user local
// process can already read ~/.monoagent/extension.token, so the claim adds
// nothing for it). Set MONOAGENT_EXTENSION_IDS to the store id to shut the
// first case; unpacked development builds have a path-derived id and need to
// be listed there, or auto-pairing turned off.

const (
	// AutoPairEnv, set to 0/false/off/no, turns the claim flow off without a database.
	AutoPairEnv = "MONOAGENT_EXTENSION_AUTOPAIR"
	// ExtensionIDsEnv is a comma-separated list of extension ids allowed to claim.
	ExtensionIDsEnv = "MONOAGENT_EXTENSION_IDS"

	claimsPerWindow = 5
	claimWindow     = time.Minute
	maxClaimBody    = 4 << 10
)

// SetAutoPair installs the kill-switch reader (`extension autopair`).
// nil, the default, means on.
func (s *Server) SetAutoPair(enabled func() bool) {
	s.claimMu.Lock()
	s.autoPair = enabled
	s.claimMu.Unlock()
}

func (s *Server) autoPairOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(AutoPairEnv))) {
	case "0", "false", "off", "no":
		return false
	}
	s.claimMu.Lock()
	fn := s.autoPair
	s.claimMu.Unlock()
	return fn == nil || fn()
}

// claimOrigin returns the extension id of an Origin header, and whether it is
// an extension origin at all.
func claimOrigin(origin string) (id string, ok bool) {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "chrome-extension" && u.Scheme != "moz-extension") || u.Host == "" {
		return "", false
	}
	return u.Host, true
}

func extensionIDAllowed(id string) bool {
	raw := strings.TrimSpace(os.Getenv(ExtensionIDsEnv))
	if raw == "" {
		return true
	}
	for _, a := range strings.Split(raw, ",") {
		if strings.TrimSpace(a) == id {
			return true
		}
	}
	return false
}

func loopbackPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// claimAllowed runs the checks that do not need the account or the rate
// limit, and returns the reason it refuses ("" when it does not).
func claimAllowed(r *http.Request) (originID, refusal string) {
	if !loopbackPeer(r) || !loopbackHost(r.Host) {
		return "", "not a loopback request"
	}
	id, ok := claimOrigin(r.Header.Get("Origin"))
	if !ok {
		return "", "not from a browser extension"
	}
	if !extensionIDAllowed(id) {
		return "", "this extension id is not allowed to pair"
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "same-site":
		return "", "not from an extension"
	}
	if r.Header.Get("Sec-Fetch-Mode") == "navigate" {
		return "", "not a fetch"
	}
	return id, ""
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ClaimStatus is what GET /monoagent/status answers: only enough for an
// extension to decide what to show. No names, no addresses, no token.
type ClaimStatus struct {
	Service  string `json:"service"`
	SignedIn bool   `json:"signedIn"`
	Paired   bool   `json:"paired"`
	AutoPair bool   `json:"autoPair"`
}

// handleClaimStatus answers /monoagent/status.
func (s *Server) handleClaimStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !loopbackPeer(r) || !loopbackHost(r.Host) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" {
		if _, ok := claimOrigin(o); !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	writeJSONStatus(w, http.StatusOK, ClaimStatus{
		Service:  ServiceName,
		SignedIn: !accountRefuses(),
		Paired:   s.IsConnected(),
		AutoPair: s.autoPairOn(),
	})
}

type claimRequest struct {
	Instance string `json:"instance"`
	Label    string `json:"label"`
}

type claimReply struct {
	Token string `json:"token"`
	ID    string `json:"id"`
}

// uaHints are tried in order: Edge and Opera also say Chrome.
var uaHints = []*regexp.Regexp{
	regexp.MustCompile(`Edg/[\d.]+`), regexp.MustCompile(`OPR/[\d.]+`),
	regexp.MustCompile(`Firefox/[\d.]+`), regexp.MustCompile(`Chrome/[\d.]+`),
}

// allowClaim is the sliding-window rate limit.
func (s *Server) allowClaim(now time.Time) bool {
	s.claimMu.Lock()
	defer s.claimMu.Unlock()
	kept := s.claimTimes[:0]
	for _, t := range s.claimTimes {
		if now.Sub(t) < claimWindow {
			kept = append(kept, t)
		}
	}
	s.claimTimes = kept
	if len(s.claimTimes) >= claimsPerWindow {
		return false
	}
	s.claimTimes = append(s.claimTimes, now)
	return true
}

// handlePairClaim answers POST /monoagent/pair/claim.
func (s *Server) handlePairClaim(w http.ResponseWriter, r *http.Request) {
	refuse := func(code int, reason string) {
		s.logger.Warn().Str("remote", r.RemoteAddr).Str("origin", r.Header.Get("Origin")).Str("reason", reason).Msg("browser pairing claim refused")
		writeJSONStatus(w, code, map[string]string{"error": reason})
	}
	if r.Method != http.MethodPost {
		refuse(http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.autoPairOn() {
		refuse(http.StatusForbidden, "automatic pairing is turned off: run monoagentcli extension pair")
		return
	}
	originID, why := claimAllowed(r)
	if why != "" {
		refuse(http.StatusForbidden, why)
		return
	}
	if accountRefuses() {
		refuse(http.StatusForbidden, "signed out: sign in to MonoAgent first")
		return
	}
	if !s.allowClaim(time.Now()) {
		w.Header().Set("Retry-After", "60")
		refuse(http.StatusTooManyRequests, "too many pairing attempts; try again in a minute")
		return
	}
	var req claimRequest
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxClaimBody))
	_ = json.Unmarshal(body, &req)
	if !validInstanceID.MatchString(req.Instance) {
		req.Instance = ""
	}
	agent := ""
	for _, re := range uaHints {
		if m := re.FindString(r.Header.Get("User-Agent")); m != "" {
			agent = m
			break
		}
	}
	label := cleanLabel(req.Label)
	if label == "" {
		label = agent
	}
	if label == "" {
		label = "browser " + originID
	}
	token, entry, err := addPairedBrowser(PairedBrowser{
		Label: label, Origin: r.Header.Get("Origin"), Agent: agent, Instance: req.Instance,
	})
	if err != nil {
		s.logger.Error().Err(err).Msg("browser pairing claim failed")
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "could not record the pairing"})
		return
	}
	s.logger.Info().Str("browser", entry.ID).Str("label", entry.Label).Str("origin", entry.Origin).
		Msg("browser paired automatically (revoke with: monoagentcli extension browsers revoke " + entry.ID + ")")
	writeJSONStatus(w, http.StatusOK, claimReply{Token: token, ID: entry.ID})
}

// sweepRevoked closes the sockets whose per-browser token has been revoked.
func (s *Server) sweepRevoked() {
	live := pairedBrowserIDs()
	s.connMu.Lock()
	var drop []*extConn
	for _, c := range s.conns {
		if c.tokenID != "" && !live[c.tokenID] {
			drop = append(drop, c)
		}
	}
	s.connMu.Unlock()
	for _, c := range drop {
		s.logger.Warn().Str("browser", c.tokenID).Msg("closing the socket of a revoked browser")
		_ = c.ws.Close()
	}
}
