package extension

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/monoes/mono-agent/internal/account/accounttest"
)

const extOrigin = "chrome-extension://abcdefghijklmnopabcdefghijklmnop"

// claimReq is a claim that passes every check; each test spoils one thing.
func claimReq(mut func(*http.Request)) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9222/monoagent/pair/claim",
		strings.NewReader(`{"instance":"0123456789abcdef","label":"Edge Work"}`))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("Origin", extOrigin)
	r.Header.Set("User-Agent", "Mozilla/5.0 Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0")
	if mut != nil {
		mut(r)
	}
	return r
}

func newClaimServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv(AutoPairEnv, "")
	t.Setenv(ExtensionIDsEnv, "")
	return NewServer("127.0.0.1:9222", zerolog.Nop())
}

func claim(srv *Server, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.handlePairClaim(w, r)
	return w
}

// A claim is refused, with no token in the reply and nothing recorded, for
// every way it can come from somewhere that is not the extension.
func TestPairClaimRefusalMatrix(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*http.Request)
		code int
	}{
		{"web page origin", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"loopback web page origin", func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:3000") }, 403},
		{"no origin", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"opaque origin", func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"extension origin without id", func(r *http.Request) { r.Header.Set("Origin", "chrome-extension://") }, 403},
		{"rebound host", func(r *http.Request) { r.Host = "evil.example:9222" }, 403},
		{"non-loopback peer", func(r *http.Request) { r.RemoteAddr = "192.0.2.7:4000" }, 403},
		{"same-origin fetch", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") }, 403},
		{"navigation", func(r *http.Request) { r.Header.Set("Sec-Fetch-Mode", "navigate") }, 403},
		{"GET", func(r *http.Request) { r.Method = http.MethodGet }, 405},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			accounttest.Install(t, accounttest.SignedIn)
			srv := newClaimServer(t)
			w := claim(srv, claimReq(c.mut))
			if w.Code != c.code || strings.Contains(w.Body.String(), `"token"`) {
				t.Fatalf("claim = %d %s, want %d and no token", w.Code, w.Body, c.code)
			}
			if list, _ := ListPairedBrowsers(); len(list) != 0 {
				t.Fatalf("a refused claim recorded %v", list)
			}
		})
	}
}

func TestPairClaimSignedOutIsRefused(t *testing.T) {
	for _, mode := range []accounttest.Mode{accounttest.LockedNoLogin, accounttest.LockedRefused} {
		accounttest.Install(t, mode)
		srv := newClaimServer(t)
		w := claim(srv, claimReq(nil))
		if w.Code != 403 || strings.Contains(w.Body.String(), `"token"`) || !strings.Contains(w.Body.String(), "signed out") {
			t.Fatalf("signed-out claim = %d %s", w.Code, w.Body)
		}
	}
}

func TestPairClaimKillSwitch(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	srv.SetAutoPair(func() bool { return false })
	if w := claim(srv, claimReq(nil)); w.Code != 403 || !strings.Contains(w.Body.String(), "extension pair") {
		t.Fatalf("claim with the switch off = %d %s", w.Code, w.Body)
	}
	srv.SetAutoPair(func() bool { return true })
	if w := claim(srv, claimReq(nil)); w.Code != 200 {
		t.Fatalf("claim with the switch on = %d %s", w.Code, w.Body)
	}
	srv.SetAutoPair(nil) // default: on
	t.Setenv(AutoPairEnv, "0")
	if w := claim(srv, claimReq(nil)); w.Code != 403 {
		t.Fatalf("claim with %s=0 = %d", AutoPairEnv, w.Code)
	}
}

func TestPairClaimExtensionIDList(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	t.Setenv(ExtensionIDsEnv, "someotherextensionidsomeotherextens, "+strings.TrimPrefix(extOrigin, "chrome-extension://"))
	if w := claim(srv, claimReq(nil)); w.Code != 200 {
		t.Fatalf("listed id = %d %s", w.Code, w.Body)
	}
	t.Setenv(ExtensionIDsEnv, "someotherextensionidsomeotherextens")
	if w := claim(srv, claimReq(nil)); w.Code != 403 {
		t.Fatalf("unlisted id = %d %s", w.Code, w.Body)
	}
}

func TestPairClaimRateLimit(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	for i := 0; i < claimsPerWindow; i++ {
		if w := claim(srv, claimReq(nil)); w.Code != 200 {
			t.Fatalf("claim %d = %d %s", i, w.Code, w.Body)
		}
	}
	w := claim(srv, claimReq(nil))
	if w.Code != 429 || strings.Contains(w.Body.String(), `"token"`) || w.Header().Get("Retry-After") == "" {
		t.Fatalf("over the limit = %d %s", w.Code, w.Body)
	}
	// Refused requests (a web page) do not use up the quota of the real one.
	srv2 := newClaimServer(t)
	for i := 0; i < 3*claimsPerWindow; i++ {
		claim(srv2, claimReq(func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }))
	}
	if w := claim(srv2, claimReq(nil)); w.Code != 200 {
		t.Fatalf("claim after refused ones = %d %s", w.Code, w.Body)
	}
}

func TestPairClaimGrantsAPerBrowserTokenThatRevokes(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv, base := startPairingTestServer(t)
	_ = srv
	w := claim(srv, claimReq(nil))
	if w.Code != 200 {
		t.Fatalf("claim = %d %s", w.Code, w.Body)
	}
	var got claimReply
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || len(got.Token) != 64 || got.ID == "" {
		t.Fatalf("reply = %s (%v)", w.Body, err)
	}
	if got.Token == srv.token {
		t.Fatal("the claim handed out the shared extension.token")
	}
	list, _ := ListPairedBrowsers()
	if len(list) != 1 || list[0].Label != "Edge Work" || list[0].Origin != extOrigin || list[0].Agent != "Edg/140.0.0.0" {
		t.Fatalf("recorded = %+v", list)
	}
	if strings.Contains(mustRead(t, browsersPathT(t)), got.Token) {
		t.Fatal("the browsers file holds the token itself, not its hash")
	}

	dial := func(token string) (*websocket.Conn, bool) {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/monoagent", http.Header{"Origin": {extOrigin}})
		if err != nil {
			t.Fatal(err)
		}
		_ = c.WriteJSON(map[string]string{"type": "auth", "token": token, "instance": "0123456789abcdef"})
		c.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		_, _, err = c.ReadMessage()
		ce, closed := err.(*websocket.CloseError)
		return c, !(closed && ce.Code == unauthorizedCloseCode)
	}
	conn, ok := dial(got.Token)
	if !ok {
		t.Fatal("the claimed token was refused by the handshake")
	}
	defer conn.Close()
	if !srv.IsConnected() {
		t.Fatal("the claimed browser is not connected")
	}

	if _, err := RevokePairedBrowser(got.ID[:6]); err != nil {
		t.Fatal(err)
	}
	if _, ok := dial(got.Token); ok {
		t.Fatal("a revoked token still authenticates")
	}
	srv.sweepRevoked()
	deadline := time.Now().Add(2 * time.Second)
	for srv.IsConnected() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if srv.IsConnected() {
		t.Fatal("the socket of a revoked browser stayed connected")
	}
	if _, err := RevokePairedBrowser(got.ID); err == nil {
		t.Fatal("revoking twice should fail")
	}
}

func TestPairClaimReplacesTheSameInstance(t *testing.T) {
	accounttest.Install(t, accounttest.SignedIn)
	srv := newClaimServer(t)
	var first, second claimReply
	_ = json.Unmarshal(claim(srv, claimReq(nil)).Body.Bytes(), &first)
	_ = json.Unmarshal(claim(srv, claimReq(nil)).Body.Bytes(), &second)
	if _, ok := pairedBrowserForToken(first.Token); ok {
		t.Fatal("the earlier token of the same browser still works")
	}
	if _, ok := pairedBrowserForToken(second.Token); !ok {
		t.Fatal("the new token does not work")
	}
}

func TestClaimStatus(t *testing.T) {
	srv := newClaimServer(t)
	get := func(mut func(*http.Request)) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9222/monoagent/status", nil)
		r.RemoteAddr = "127.0.0.1:50000"
		if mut != nil {
			mut(r)
		}
		w := httptest.NewRecorder()
		srv.handleClaimStatus(w, r)
		return w
	}
	accounttest.Install(t, accounttest.SignedIn)
	var st ClaimStatus
	w := get(func(r *http.Request) { r.Header.Set("Origin", extOrigin) })
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || !st.SignedIn || st.Service != ServiceName || !st.AutoPair || st.Paired {
		t.Fatalf("signed in status = %s", w.Body)
	}
	if strings.Count(w.Body.String(), ":") != 4 {
		t.Fatalf("status says more than it should: %s", w.Body)
	}
	accounttest.Install(t, accounttest.LockedNoLogin)
	_ = json.Unmarshal(get(nil).Body.Bytes(), &st)
	if st.SignedIn {
		t.Fatal("signed out status says signed in")
	}
	for name, mut := range map[string]func(*http.Request){
		"web origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"rebound":    func(r *http.Request) { r.Host = "evil.example" },
		"remote":     func(r *http.Request) { r.RemoteAddr = "192.0.2.1:1" },
	} {
		if w := get(mut); w.Code != 403 {
			t.Errorf("%s status = %d, want 403", name, w.Code)
		}
	}
}

func browsersPathT(t *testing.T) string {
	t.Helper()
	p, err := browsersPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
