package mcpnode

import (
	"context"
	"net/http"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// An MCP server's configured headers carry its key: a 307 that would
// resend the JSON-RPC body to another host isn't followed, and a 302
// follows without them.
func TestMCPRequestsKeepTheHeadersOnRedirect(t *testing.T) {
	post := credhttptest.NewPair(t, http.StatusTemporaryRedirect, `{}`)
	tr := newHTTPTransport(post.From+"/mcp", map[string]string{"X-Api-Key": "mcp-k3y"})
	if resp, err := tr.post(context.Background(), []byte(`{"jsonrpc":"2.0"}`)); err == nil {
		resp.Body.Close()
	}
	if got := post.Got(); len(got) != 0 {
		t.Errorf("a POST's 307 reached the other host: %+v", got)
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
