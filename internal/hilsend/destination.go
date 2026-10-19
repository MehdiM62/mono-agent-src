package hilsend

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// sendHost says where a node type's config names the server its sends go
// through: the keys, the port key, and the platform's own server, which the
// card leaves out. Guard asks with the type that runs the node
// (workflow.CanonicalNodeType), so a legacy name needs no entry.
type sendHost struct {
	keys     []string
	port     string
	platform string
}

var sendHosts = map[string]sendHost{
	"comm.email_send":  {keys: []string{"smtp_host"}, port: "smtp_port"},
	"service.mastodon": {keys: []string{"instance", "instance_url"}, platform: "mastodon.social"},
}

// hostOf is the server a send goes through, for the card, when its config
// names one that isn't the platform's own: the mail relay, the Mastodon
// instance.
func hostOf(nodeType string, config map[string]interface{}) string {
	h, ok := sendHosts[nodeType]
	if !ok {
		return ""
	}
	_, raw := firstKey(config, h.keys)
	host := raw
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		host = u.Host
	}
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "/"))
	if host == "" || host == h.platform {
		return ""
	}
	if p := str(config[h.port]); p != "" && !strings.Contains(host, ":") {
		host += ":" + p
	}
	if !cardHost.MatchString(host) {
		return strconv.QuoteToASCII(host)
	}
	return host
}

// cardHost is a host[:port] the card shows as it is; anything else shows
// quoted (OneLine).
var cardHost = regexp.MustCompile(`^([a-z0-9.-]+|\[[0-9a-f:.]+\])(:[0-9]{1,5})?$`)

// OneLine is s as a card line shows it: as it is when it is printable
// ASCII, else quoted with its control, escape and other non-ASCII
// characters escaped, so a value can't add a line to the card ("Prepared
// by: …") or move the terminal's cursor.
func OneLine(s string) string {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return strconv.QuoteToASCII(s)
		}
	}
	return s
}

// fetchURLOf is the URL the platform fetches what a send says from, for
// the card: a Twilio call's TwiML.
func fetchURLOf(nodeType string, config map[string]interface{}) string {
	if nodeType == "comm.twilio" && workflow.ConfigOperation(config) == "make_call" {
		return OneLine(str(config["url"]))
	}
	return ""
}
