// Package credhttp is the HTTP client policy for requests that carry a
// credential: a token, a password, an API key, a client secret, a
// signature. A redirect that leaves the request's host, or goes from https
// to plain http, carries none of the credential (Referer included), and
// one that would send the request's body there again (307, 308) isn't
// followed: the request fails with ErrResend, naming the host. Go's own
// policy drops only Authorization and cookies on another domain (a
// subdomain and another port count as the same), keeps an X-Api-Key and
// the Referer, keeps everything on an https-to-http hop of the same host,
// and resends a 307's body anywhere.
//
// Client gives a client whose every request counts as carrying one (a
// token exchange, a platform's API with the user's token); CheckRedirect
// is the policy for a client that sends both kinds, applied to the
// requests Mark marks.
package credhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type markKey struct{}

// mark names the headers a credential put on a request.
type mark struct{ headers []string }

// Mark records that req carries a credential: in the headers named, and
// perhaps in its body or query.
func Mark(req *http.Request, headers ...string) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), markKey{}, &mark{headers}))
}

// SetBy names the headers whose values differ between before and after:
// the ones a signer or an injector set.
func SetBy(before, after http.Header) []string {
	var names []string
	for k, v := range after {
		if !slices.Equal(before[k], v) {
			names = append(names, k)
		}
	}
	return names
}

// Client returns a copy of base (a zero client when nil) whose requests
// all count as carrying a credential: one Mark didn't mark carries it in
// every header but the standard ones (standardHeaders). A CheckRedirect
// base already has is kept and runs first: its refusal stands, and the
// credential policy applies to what it lets through.
func Client(base *http.Client) *http.Client {
	c := http.Client{}
	if base != nil {
		c = *base
	}
	if own := c.CheckRedirect; own != nil {
		c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if err := own(req, via); err != nil {
				return err
			}
			return everyRequest(req, via)
		}
	} else {
		c.CheckRedirect = everyRequest
	}
	return &c
}

// CheckRedirect is the policy for a request Mark marked; any other
// follows as Go's default does, without Authorization once it leaves the
// host. It stops after 10 redirects, as Go's default does.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	return check(req, via, false)
}

func everyRequest(req *http.Request, via []*http.Request) error {
	return check(req, via, true)
}

// ErrResend is wrapped by the error of a redirect that isn't followed
// because it would send the request's body, and with it the credential,
// to another host.
var ErrResend = errors.New("not followed because it would resend the credential")

func check(req *http.Request, via []*http.Request, credentialed bool) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	first := via[0]
	if !Leaves(first.URL, req.URL) {
		return nil
	}
	m, marked := first.Context().Value(markKey{}).(*mark)
	if marked || credentialed {
		if req.Body != nil && req.Body != http.NoBody {
			return fmt.Errorf("redirected to %s, %w", req.URL.Host, ErrResend)
		}
		names := standardless(first.Header)
		if marked {
			names = m.headers
		}
		// Referer: Go sets it to the URL being left, which may hold the
		// credential (a key in the query, a token in the path, a
		// pre-signed URL).
		del(req.Header, append(names, "Proxy-Authorization", "Cookie", "Referer")...)
	}
	del(req.Header, "Authorization")
	return nil
}

// del deletes every key of h that is one of names however it is spelled:
// Header.Del canonicalises its argument but not a key stored as written
// (h["x-api-key"]).
func del(h http.Header, names ...string) {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[http.CanonicalHeaderKey(n)] = true
	}
	for k := range h {
		if drop[http.CanonicalHeaderKey(k)] {
			delete(h, k)
		}
	}
}

// Leaves reports whether a redirect from from to to leaves the request's
// host: another host or port, or https to anything else.
func Leaves(from, to *url.URL) bool {
	return !strings.EqualFold(from.Host, to.Host) ||
		(strings.EqualFold(from.Scheme, "https") && !strings.EqualFold(to.Scheme, "https"))
}

// standardHeaders carry nothing of a credential.
var standardHeaders = map[string]bool{
	"Accept": true, "Accept-Encoding": true, "Accept-Language": true, "Cache-Control": true,
	"Content-Type": true, "Content-Length": true, "User-Agent": true,
	"If-None-Match": true, "If-Modified-Since": true, "Range": true, "Last-Event-Id": true,
}

// standardless names h's headers but the standard ones.
func standardless(h http.Header) []string {
	var names []string
	for k := range h {
		if !standardHeaders[http.CanonicalHeaderKey(k)] {
			names = append(names, k)
		}
	}
	return names
}
