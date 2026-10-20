package mcpnode

import (
	"context"
	"errors"
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
