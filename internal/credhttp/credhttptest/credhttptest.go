// Package credhttptest gives the two local servers a credhttp test needs:
// one that redirects every request to the other, which records what
// reaches it.
package credhttptest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Seen is one request the other server got.
type Seen struct {
	Method, Path string
	Header       http.Header
	Body         string
}

// Pair is a redirecting server (From) and the server it redirects to.
type Pair struct {
	From  string // the redirecting server's URL
	To    string // the other server's URL
	mu    sync.Mutex
	seen  []Seen
	reply string
}

// NewPair starts the two servers: From answers every request with a
// redirect of code to the same path on To, and To answers reply (JSON).
func NewPair(t *testing.T, code int, reply string) *Pair {
	t.Helper()
	p := &Pair{reply: reply}
	to := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.seen = append(p.seen, Seen{r.Method, r.URL.Path, r.Header.Clone(), string(b)})
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(p.reply))
	}))
	t.Cleanup(to.Close)
	from := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, to.URL+r.URL.RequestURI(), code)
	}))
	t.Cleanup(from.Close)
	p.From, p.To = from.URL, to.URL
	return p
}

// Got is what reached To.
func (p *Pair) Got() []Seen {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Seen(nil), p.seen...)
}

// Leaked reports the first request To got that holds secret in a header
// or its body ("" for none).
func (p *Pair) Leaked(secret string) string {
	for _, s := range p.Got() {
		for k, vs := range s.Header {
			for _, v := range vs {
				if strings.Contains(v, secret) {
					return s.Method + " " + s.Path + " header " + k
				}
			}
		}
		if strings.Contains(s.Body, secret) {
			return s.Method + " " + s.Path + " body"
		}
	}
	return ""
}
