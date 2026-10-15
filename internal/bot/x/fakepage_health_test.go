//go:build !nosocial

package x

// An offline fake of browser.PageInterface: page scripts are answered from
// canned values (testdata/health_*.json) chosen by matching the script, so
// the Go side of every bot method runs without a browser. The fake does not
// implement EvalCDP or CDP, so botpkg.EvalJSON falls back to Eval and
// ClickTrusted/TypeInto fall back to ElementHandle.Click/Input.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
)

// healthReply answers one page script; call is the 1-based number of times
// this rule has matched so far.
type healthReply func(call int, args []interface{}) (interface{}, error)

type healthRule struct {
	name  string
	match func(js string) bool
	reply healthReply
}

type healthPage struct {
	browser.PageInterface // nil: any method not overridden panics

	mu        sync.Mutex
	url       string
	urlErr    error
	redirects map[string]string // navigation target -> URL the page lands on
	navs      []string
	rules     []healthRule
	calls     map[string]int
	unknown   int
	has       map[string]bool
	clicks    []string
	inputs    []string
	files     [][]string
	keys      []rune
}

func newHealthPage(url string) *healthPage {
	return &healthPage{url: url, redirects: map[string]string{}, calls: map[string]int{}, has: map[string]bool{}}
}

// on answers the script equal to js (the bot's script constant).
func (p *healthPage) on(name, js string, r healthReply) *healthPage {
	p.rules = append(p.rules, healthRule{name: name, match: func(s string) bool { return s == js }, reply: r})
	return p
}

func (p *healthPage) count(name string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[name]
}

func (p *healthPage) Eval(js string, args ...interface{}) (*browser.EvalResult, error) {
	p.mu.Lock()
	var rule *healthRule
	for i := range p.rules {
		if p.rules[i].match(js) {
			rule = &p.rules[i]
			break
		}
	}
	if rule == nil {
		p.unknown++
		p.mu.Unlock()
		return nil, errors.New("health: unexpected page script")
	}
	p.calls[rule.name]++
	n := p.calls[rule.name]
	p.mu.Unlock()
	v, err := rule.reply(n, args)
	if err != nil {
		return nil, err
	}
	return browser.NewEvalResult(v), nil
}

func (p *healthPage) Navigate(u string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.navs = append(p.navs, u)
	if to, ok := p.redirects[u]; ok {
		u = to
	}
	p.url = u
	return nil
}

func (p *healthPage) WaitLoad() error { return nil }

func (p *healthPage) GetURL() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.url, p.urlErr
}

func (p *healthPage) Has(sel string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.has[sel], nil
}

func (p *healthPage) Element(sel string, _ time.Duration) (browser.ElementHandle, error) {
	return &healthElem{p: p, sel: sel}, nil
}

func (p *healthPage) KeyboardPress(k rune) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = append(p.keys, k)
	return nil
}

type healthElem struct {
	browser.ElementHandle
	p   *healthPage
	sel string
}

func (e *healthElem) Click() error {
	e.p.mu.Lock()
	defer e.p.mu.Unlock()
	e.p.clicks = append(e.p.clicks, e.sel)
	return nil
}

func (e *healthElem) Input(text string) error {
	e.p.mu.Lock()
	defer e.p.mu.Unlock()
	e.p.inputs = append(e.p.inputs, text)
	return nil
}

func (e *healthElem) SetFiles(paths []string) error {
	e.p.mu.Lock()
	defer e.p.mu.Unlock()
	e.p.files = append(e.p.files, paths)
	return nil
}

func (e *healthElem) Focus() error { return nil }

// healthConst always answers v.
func healthConst(v interface{}) healthReply {
	return func(int, []interface{}) (interface{}, error) { return v, nil }
}

// healthSeq answers vs[call-1], repeating the last value.
func healthSeq(vs ...interface{}) healthReply {
	return func(n int, _ []interface{}) (interface{}, error) {
		if n > len(vs) {
			n = len(vs)
		}
		return vs[n-1], nil
	}
}

// healthFixture returns scenario key of testdata/<file> as raw JSON.
func healthFixture(t *testing.T, file, key string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	v, ok := all[key]
	if !ok {
		t.Fatalf("%s: no scenario %q", file, key)
	}
	return v
}

// healthFast shortens the bot's waits so timeouts take milliseconds.
func healthFast(t *testing.T) {
	t.Helper()
	old := []time.Duration{loadTimeout, pollInterval, verifyTimeout, scrollSettle, actionPause}
	loadTimeout, pollInterval, verifyTimeout, scrollSettle, actionPause = 200*time.Millisecond, 5*time.Millisecond, 200*time.Millisecond, 0, 0
	t.Cleanup(func() {
		loadTimeout, pollInterval, verifyTimeout, scrollSettle, actionPause = old[0], old[1], old[2], old[3], old[4]
	})
}

func healthWantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one containing %q", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err, substr)
	}
}
