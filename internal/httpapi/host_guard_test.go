package httpapi

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// serveGuarded serves a test server on a loopback listener and returns its port.
func serveGuarded(t *testing.T, s *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = s.Serve(ctx, ln); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

func getWithHost(t *testing.T, port, host, path string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestRebindingHostIsRefusedOnEveryRoute(t *testing.T) {
	s := newTestServer(t, true)
	port := serveGuarded(t, s)
	paths := []string{"/health", "/healthz", "/nodes", "/profiles/default/workflows", "/profiles/default/hil", "/profiles/nope/workflows", "/v1/models", "/nothing"}
	for _, bad := range []string{"evil.example:" + port, "evil.example", "127.0.0.1.evil.example:" + port, "127.0.0.1", "127.0.0.1:1", "192.168.1.5:" + port} {
		for _, p := range paths {
			if got := getWithHost(t, port, bad, p); got != http.StatusMisdirectedRequest {
				t.Errorf("Host %q GET %s: status %d, want 421", bad, p, got)
			}
		}
	}
}

func TestLegitimateHostIsServed(t *testing.T) {
	s := newTestServer(t, false)
	port := serveGuarded(t, s)
	for _, ok := range []string{"127.0.0.1:" + port, "localhost:" + port, "LocalHost:" + port, "[::1]:" + port} {
		if got := getWithHost(t, port, ok, "/health"); got != http.StatusOK {
			t.Errorf("Host %q /health: status %d, want 200", ok, got)
		}
	}
	// Past the guard the usual door applies: a profile route without a token is 401, not 421.
	if got := getWithHost(t, port, "127.0.0.1:"+port, "/profiles/default/workflows"); got != http.StatusUnauthorized {
		t.Errorf("profile route without a token: %d, want 401", got)
	}
}

func TestConfiguredHostIsAllowedOnlyWhenListed(t *testing.T) {
	s := newTestServer(t, false)
	s.opts.AllowedHosts = []string{"Agent.Internal:9322"}
	port := serveGuarded(t, s)
	if got := getWithHost(t, port, "agent.internal:9322", "/health"); got != http.StatusOK {
		t.Errorf("listed host: %d, want 200", got)
	}
	if got := getWithHost(t, port, "other.internal:9322", "/health"); got != http.StatusMisdirectedRequest {
		t.Errorf("unlisted host: %d, want 421", got)
	}
}

func TestEnvListedHostAndWildcardBind(t *testing.T) {
	t.Setenv(allowedHostsEnv, "nas.lan:9322, ,10.0.0.7:9322")
	got := allowedHosts("[::]:9322", configuredHosts(Options{}))
	for _, want := range []string{"nas.lan:9322", "10.0.0.7:9322", "127.0.0.1:9322", "localhost:9322", "[::1]:9322"} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	for _, bad := range []string{"[::]:9322", "0.0.0.0:9322", "evil.example:9322"} {
		if got[bad] {
			t.Errorf("%q must not be allowed", bad)
		}
	}
}

func TestNonLoopbackWarning(t *testing.T) {
	w := nonLoopbackWarning("0.0.0.0:9322", nil)
	for _, want := range []string{"0.0.0.0:9322", "421", allowedHostsEnv + "=host:port"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning %q lacks %q", w, want)
		}
	}
	if nonLoopbackWarning("[::]:9322", nil) == "" {
		t.Error("no warning for a wildcard bind")
	}
	if nonLoopbackWarning("127.0.0.1:9322", nil) != "" || nonLoopbackWarning("[::1]:9322", nil) != "" {
		t.Error("warning on a loopback bind")
	}
	if nonLoopbackWarning("0.0.0.0:9322", []string{"nas.lan:9322"}) != "" {
		t.Error("warning although hosts are configured")
	}
}

func TestServeWarnsOnceOnNonLoopbackBind(t *testing.T) {
	s := newTestServer(t, false)
	var buf bytes.Buffer
	s.warnOut = &buf
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Skip("cannot bind a wildcard address")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = s.Serve(ctx, ln); close(done) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	if n := strings.Count(buf.String(), "get 421"); n != 1 {
		t.Errorf("want one 421 warning, got %d in %q", n, buf.String())
	}
}

func TestRefusalBodyNamesTheSetting(t *testing.T) {
	s := newTestServer(t, false)
	port := serveGuarded(t, s)
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+"/health", nil)
	req.Host = "evil.example"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), allowedHostsEnv) {
		t.Errorf("421 body does not say how to allow a host: %s", b)
	}
}
