//go:build !nosocial

package linkedin

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

// healthCall is what a scripted page function sees: how often its rule
// matched before (N, from 0), the page's current URL and the call's args.
type healthCall struct {
	N    int
	URL  string
	Args []interface{}
}

type healthRule struct {
	sub string
	fn  func(c healthCall) interface{}
}

// healthPage is an offline browser.PageInterface: Eval answers each page
// function with a canned value picked by a distinctive substring of its
// source. Unscripted functions fail (and are recorded), like a page script
// that threw. Methods the bot code does not reach panic via the nil
// embedded interface.
type healthPage struct {
	browser.PageInterface

	mu      sync.Mutex
	url     string
	navs    []string
	rules   []healthRule
	hits    map[string]int
	unknown []string
	has     map[string]bool
	navErr  error
}

func newHealthPage(t *testing.T) *healthPage {
	t.Helper()
	p := &healthPage{hits: map[string]int{}, has: map[string]bool{}}
	t.Cleanup(func() {
		for _, u := range p.unknown {
			t.Logf("unscripted page function: %s", u)
		}
	})
	return p
}

// on scripts the page functions whose source contains sub.
func (p *healthPage) on(sub string, fn func(c healthCall) interface{}) *healthPage {
	p.rules = append(p.rules, healthRule{sub, fn})
	return p
}

// onValue scripts a constant answer.
func (p *healthPage) onValue(sub string, v interface{}) *healthPage {
	return p.on(sub, func(healthCall) interface{} { return v })
}

func (p *healthPage) hitCount(sub string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits[sub]
}

func (p *healthPage) navigations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.navs...)
}

func (p *healthPage) Navigate(u string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.navErr != nil {
		return p.navErr
	}
	p.url = u
	p.navs = append(p.navs, u)
	return nil
}

func (p *healthPage) WaitLoad() error { return nil }

func (p *healthPage) GetURL() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.url, nil
}

func (p *healthPage) Has(sel string) (bool, error) { return p.has[sel], nil }

func (p *healthPage) MouseScroll(x, y float64, steps int) error { return nil }

func (p *healthPage) Element(sel string, d time.Duration) (browser.ElementHandle, error) {
	return nil, errors.New("no element handles on the health fake")
}

func (p *healthPage) Eval(js string, args ...interface{}) (*browser.EvalResult, error) {
	p.mu.Lock()
	var rule *healthRule
	for i := range p.rules {
		if strings.Contains(js, p.rules[i].sub) {
			rule = &p.rules[i]
			break
		}
	}
	if rule == nil {
		body := js
		if i := strings.Index(js, "\nreturn ("); i >= 0 {
			body = js[i:]
		}
		if len(body) > 160 {
			body = body[:160]
		}
		p.unknown = append(p.unknown, body)
		p.mu.Unlock()
		return nil, errors.New("unscripted page function")
	}
	c := healthCall{N: p.hits[rule.sub], URL: p.url, Args: args}
	p.hits[rule.sub]++
	p.mu.Unlock()
	v := rule.fn(c)
	if err, ok := v.(error); ok {
		return nil, err
	}
	return browser.NewEvalResult(v), nil
}

// healthFixture loads testdata/health_<name>.json as a raw JSON value, the
// shape the page function returns.
func healthFixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "health_"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(b) {
		t.Fatalf("fixture %s is not valid JSON", name)
	}
	return json.RawMessage(b)
}

// healthTimings makes every wait and pause near-instant.
func healthTimings(t *testing.T) {
	t.Helper()
	old := []time.Duration{pageSettle, uiSettle, pollEvery, findTimeout, verifyTimeout, scrollSettle, emptySectionWait}
	pageSettle, uiSettle, pollEvery = 0, 0, time.Millisecond
	findTimeout, verifyTimeout, scrollSettle, emptySectionWait = 60*time.Millisecond, 60*time.Millisecond, 0, 5*time.Millisecond
	t.Cleanup(func() {
		pageSettle, uiSettle, pollEvery, findTimeout, verifyTimeout, scrollSettle, emptySectionWait = old[0], old[1], old[2], old[3], old[4], old[5], old[6]
	})
}

// Distinctive substrings of the page functions the read paths run.
const (
	hsProfile      = "isSelf:"
	hsScrollAbout  = "else window.scrollBy(0, window.innerHeight);"
	hsSection      = "DetailsSection"
	hsScrollPage   = "window.innerHeight * 2"
	hsPeople       = "followsBack"
	hsListEmpty    = "not following anyone"
	hsShowMore     = "scaffold-finite-scroll__load-button"
	hsUnmark       = "L.unmarkAll(tok); return true;"
	hsWaitOK       = "({ ok: !!document.querySelector("
	hsPosts        = "candidates.sort"
	hsCommentCount = "article.comments-comment-item').length"
	hsLoadMore     = "show-prev-replies"
	hsComments     = "parentId: parent ?"
	hsConvos       = "lastFromMe"
	hsFindReact    = "L.unmarkAll(tok); L.unmarkAll(tok + '-menu');"
	hsMarkedReact  = "if (!b) return { state: 'not_found' };"
	hsCommentLike  = "btns.length ? 'ambiguous' : 'no_button'"
	hsMarkedBox    = "elementFromPoint"
	hsDOMClick     = "el.click();"
)

// scriptCommon answers the housekeeping functions every list path runs:
// no "show more" button, scrolling and unmarking succeed.
func (p *healthPage) scriptCommon() *healthPage {
	return p.onValue(hsShowMore, false).onValue(hsScrollPage, true).onValue(hsUnmark, true)
}
