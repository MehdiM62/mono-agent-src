package apipkg

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/credhttp"
)

// Where a package request's credential may go: the hosts the package or
// the connection names (authHost), and on a redirect, to the same host
// only (credhttp.CheckRedirect).

// credHostKey matches a connection field that names its server.
var credHostKey = regexp.MustCompile(`(?i)(^|_)(url|host|hostname|domain|endpoint|server)$`)

// urlHost is the host of a URL, or of a bare host[:port] ("" for none).
func urlHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// literalHost finds the hosts written in a URL template, not rendered into
// it ("https://upload.box.com/…", "…then:'https://sync.api.bannerbear.com/…'").
var literalHost = regexp.MustCompile(`https?://([A-Za-z0-9.-]+)(?:[:/'"?#]|$)`)

// authHost reports whether host is one the package or the connection
// names, so the connection's secret may go to it: the host of the base URL
// as the package and connection render it (base, "" when a param or a
// response moved it), or one of its subdomains; a host the connection's
// own URL or host fields name, or one of its subdomains; a host written
// literally in the package's or the op's base URL or the op's path; or one
// of the package's auth_hosts ("*.example.com" for its subdomains).
func (r *opRun) authHost(host, base string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	within := func(named string) bool {
		named = strings.ToLower(strings.TrimSuffix(named, "."))
		return named != "" && (host == named || strings.HasSuffix(host, "."+named))
	}
	if within(urlHost(base)) {
		return true
	}
	for k, v := range r.scope.Cred {
		if s, ok := v.(string); ok && credHostKey.MatchString(k) && within(urlHost(s)) {
			return true
		}
	}
	for _, t := range []string{r.pkg.BaseURL, r.op.BaseURL, r.op.Path} {
		for _, m := range literalHost.FindAllStringSubmatch(t, -1) {
			if strings.EqualFold(strings.TrimSuffix(m[1], "."), host) {
				return true
			}
		}
	}
	for _, h := range r.pkg.AuthHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if sub, ok := strings.CutPrefix(h, "*."); ok {
			if strings.HasSuffix(host, "."+sub) {
				return true
			}
		} else if host == h {
			return true
		}
	}
	return false
}

// credentialHeaders names the headers the credential set: those injectAuth
// added or changed (before is the request's headers just before it), and
// the package's and op's headers rendered from the connection
// ("X-Account: {{ cred.account }}").
func credentialHeaders(before, after http.Header, templates ...map[string]string) []string {
	names := credhttp.SetBy(before, after)
	for _, hs := range templates {
		for k, tv := range hs {
			if strings.Contains(tv, "cred") {
				names = append(names, http.CanonicalHeaderKey(k))
			}
		}
	}
	return names
}

// checkedClient is httpClient with credhttp's redirect policy, for a
// request the credential goes on outside an op's: a token request, a
// metadata fetch. credhttp.Mark marks it.
func checkedClient() *http.Client {
	c := *httpClient
	c.CheckRedirect = credhttp.CheckRedirect
	return &c
}
