package workflow

import (
	"net"
	"os"
	"strings"
)

// publicURLEnv names the base URL the webhook server is reached at from
// outside (a tunnel or reverse proxy), e.g. https://hooks.example.com.
const publicURLEnv = "MONOAGENT_PUBLIC_URL"

// PublicBaseURL is the base of the URLs this machine hands out for its
// webhook server (reply links, resume URLs): MONOAGENT_PUBLIC_URL when set,
// otherwise the server's listen address (MONOAGENT_WEBHOOK_ADDR or the
// loopback default) — http on loopback, https elsewhere, as the server
// serves it. A wildcard bind is named by the machine's host name.
func PublicBaseURL() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv(publicURLEnv)), "/"); v != "" {
		return v
	}
	addr := strings.TrimSpace(os.Getenv("MONOAGENT_WEBHOOK_ADDR"))
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		addr = defaultWebhookAddr
		host, port, _ = net.SplitHostPort(addr)
	}
	scheme := "https"
	if isLoopbackAddr(addr) {
		scheme = "http"
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
		if name, err := os.Hostname(); err == nil && name != "" {
			host = name
		}
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}
