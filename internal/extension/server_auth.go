package extension

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// authTimeout bounds how long a newly-upgraded socket has to send its auth
// frame before the server gives up and closes it.
const authTimeout = 5 * time.Second

// unauthorizedCloseCode is sent to a socket that fails the auth handshake,
// so the extension (which knows this code) can distinguish "wrong/missing
// token, needs pairing" from a generic disconnect.
const unauthorizedCloseCode = 4401

// authFrame is the first message an extension socket must send after
// upgrade. Anything else — wrong type, wrong/missing token, or no message
// within authTimeout — gets the connection closed without ever being
// installed, so an unauthenticated client can never replace an
// already-authenticated one.
//
// Instance, Profile, Label and Version come from extension ≥1.5 (see
// chrome-extension/browser_binding.js). An older extension omits them and
// lands in the legacy slot; an older bridge ignores them.
type authFrame struct {
	Type     string `json:"type"`
	Token    string `json:"token"`
	Instance string `json:"instance,omitempty"`
	Profile  string `json:"profile,omitempty"`
	Label    string `json:"label,omitempty"`
	Version  string `json:"version,omitempty"`
}

// handleWS upgrades an incoming HTTP request to a WebSocket connection and
// authenticates it before treating it as the extension connection. The
// control channel otherwise has no identity check beyond same-machine
// Origin (see checkOrigin) — any local process could open it and either
// receive privileged automation commands meant for the real extension or
// knock the real extension's connection out from under it.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error().Err(err).Msg("websocket upgrade failed")
		return
	}
	conn.SetReadLimit(maxMessageSize)

	auth, ok := s.authenticate(conn)
	if !ok {
		_ = conn.Close()
		return
	}

	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Only reached after authenticate succeeds, so an unauthenticated
	// socket never replaces anything.
	c := newExtConn(conn, auth, time.Now())
	s.installConn(c)

	s.connOnce.Do(func() { close(s.connected) })
	s.logger.Info().Str("remote", conn.RemoteAddr().String()).Str("instance", c.id).
		Str("profile", c.boundProfile()).Msg("extension connected")

	done := make(chan struct{})
	go s.pingLoop(c, done)
	s.readLoop(c)
	close(done)
}

// authenticate reads exactly one frame from a freshly-upgraded connection
// and requires it to be a well-formed authFrame carrying the current
// extension token, compared in constant time. It never installs the connection:
// callers are responsible for installing the connection only on a true
// result.
func (s *Server) authenticate(conn *websocket.Conn) (authFrame, bool) {
	conn.SetReadDeadline(time.Now().Add(authTimeout))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		s.noteAuthFailure()
		s.logger.Warn().Err(err).Msg("extension connection closed before authenticating")
		return authFrame{}, false
	}

	var auth authFrame
	ok := json.Unmarshal(msg, &auth) == nil &&
		auth.Type == "auth" &&
		subtle.ConstantTimeCompare([]byte(auth.Token), []byte(s.token)) == 1
	if !ok {
		s.noteAuthFailure()
		s.logger.Warn().Msg("rejected extension connection: bad or missing auth token")
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(unauthorizedCloseCode, "unauthorized"),
			time.Now().Add(time.Second))
		return authFrame{}, false
	}
	return auth, true
}

// pingLoop periodically pings one browser's socket so a dead peer is
// detected via the read deadline in handleWS/readLoop instead of hanging
// indefinitely.
func (s *Server) pingLoop(c *extConn, done <-chan struct{}) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// Piggybacked on the ping: a capture whose chunks stopped
			// arriving has nobody waiting on it when it was the extension
			// flushing its queue, so something has to time it out.
			s.sweepCaptures()
			if err := c.ping(); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}
