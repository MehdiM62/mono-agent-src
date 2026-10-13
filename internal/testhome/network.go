package testhome

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// AllowNetworkEnv lets a test binary dial non-loopback hosts (live smoke
// tests). Without it, Main makes http.DefaultTransport refuse them.
const AllowNetworkEnv = "MONOAGENT_TEST_ALLOW_NETWORK"

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// blockNetwork makes http.DefaultTransport (and every transport cloned from
// it) refuse to dial anything but loopback, so a test that forgets to inject
// a fake server fails fast instead of reaching a live service.
func blockNetwork() {
	if os.Getenv(AllowNetworkEnv) == "1" {
		return
	}
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return
	}
	next := t.DialContext
	if next == nil {
		next = (&net.Dialer{}).DialContext
	}
	t.DialContext = loopbackOnly(next)
}

func loopbackOnly(next dialFunc) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !isLoopback(addr) {
			return nil, fmt.Errorf("testhome: test dialed %s %s: tests may only reach loopback (set %s=1 to allow the network)", network, addr, AllowNetworkEnv)
		}
		return next(ctx, network, addr)
	}
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
