package mcpnode

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp"
	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// An MCP server's configured headers carry its key: a 307 that would
// resend the JSON-RPC body to another host isn't followed, and a 302
// follows without them.
func TestMCPRequestsKeepTheHeadersOnRedirect(t *testing.T) {
	refuseInternal = false // both servers are on loopback; this test is about the headers
	defer func() { refuseInternal = true }()
	post := credhttptest.NewPair(t, http.StatusTemporaryRedirect, `{}`)
	tr := newHTTPTransport(post.From+"/mcp", map[string]string{"X-Api-Key": "mcp-k3y"})
	resp, err := tr.post(context.Background(), []byte(`{"jsonrpc":"2.0"}`))
	if err == nil {
		resp.Body.Close()
	}
	// The error names where the redirect went, not "answered HTTP 307".
	if got := post.Got(); len(got) != 0 || !errors.Is(err, credhttp.ErrResend) ||
		!strings.Contains(err.Error(), "redirected to "+strings.TrimPrefix(post.To, "http://")) {
		t.Errorf("a POST's 307: %v; the other host got %+v", err, got)
	}
	found := credhttptest.NewPair(t, http.StatusFound, `{}`)
	tr = newHTTPTransport(found.From+"/mcp", map[string]string{"X-Api-Key": "mcp-k3y"})
	if resp, err := tr.post(context.Background(), []byte(`{"jsonrpc":"2.0"}`)); err == nil {
		resp.Body.Close()
	}
	if got := found.Got(); len(got) != 1 || found.Leaked("mcp-k3y") != "" {
		t.Errorf("a 302 carried %+v", got)
	}
}

// A redirect from an MCP endpoint to a loopback address is not followed:
// the CLI would make that request from the user's machine (#907). The
// endpoint is a loopback test server too, so only the target differs.
func TestMCPDoesNotFollowRedirectToInternalAddress(t *testing.T) {
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther} {
		hit := false
		internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hit = true
		}))
		mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, internal.URL+"/secret", code)
		}))
		tr := newHTTPTransport(mcp.URL+"/mcp", nil)
		resp, err := tr.post(context.Background(), []byte(`{"jsonrpc":"2.0"}`))
		if err == nil {
			resp.Body.Close()
		}
		mcp.Close()
		internal.Close()
		if hit {
			t.Errorf("%d: the redirect target was requested", code)
		}
		if err == nil {
			t.Errorf("%d: the redirect was not refused", code)
		}
	}
}

// The internal-address check covers carrier-grade NAT, 0.0.0.0/8 and the
// NAT64 prefix, which reach internal hosts but are not "private" to net.IP.
func TestInternalAddressRanges(t *testing.T) {
	for ip, want := range map[string]bool{
		"100.64.0.1": true, "100.127.255.254": true, "0.1.2.3": true, "64:ff9b::7f00:1": true,
		"127.0.0.1": true, "10.0.0.1": true, "169.254.169.254": true,
		"100.128.0.1": false, "8.8.8.8": false, "2606:4700::1111": false,
	} {
		if got := isInternalIP(net.ParseIP(ip)); got != want {
			t.Errorf("isInternalIP(%s) = %v, want %v", ip, got, want)
		}
	}
}
