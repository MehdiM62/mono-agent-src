package bottest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
)

const defaultTimeout = 30 * time.Second

// Page is one browser tab. It implements browser.PageInterface with the
// semantics of *extension.ExtensionPage (see the package doc), plus the
// extension page's CDP extras: CDP, EvalCDP, TypeCDP, TypeCDPOnElement,
// InsertTextOnElement.
type Page struct {
	st      *pageState
	timeout time.Duration
	strict  bool
}

var _ browser.PageInterface = (*Page)(nil)

type pageState struct {
	b        *Browser
	t        testing.TB
	session  string
	targetID string

	mu           sync.Mutex
	loads        int
	loadCh       chan struct{}
	routes       []Route
	csp          string
	elementsWait time.Duration

	rec *Recorder
}

// NewPage opens a new tab with request interception already on (every
// request is blocked until Page.Serve adds a matching Route). The tab closes
// with the browser at test cleanup.
func (b *Browser) NewPage(t testing.TB) *Page {
	t.Helper()
	res, err := b.send("", "Target.createTarget", map[string]interface{}{"url": "about:blank"}, 0)
	if err != nil {
		t.Fatalf("bottest: %v", err)
	}
	var target struct {
		TargetID string `json:"targetId"`
	}
	_ = json.Unmarshal(res, &target)
	res, err = b.send("", "Target.attachToTarget", map[string]interface{}{"targetId": target.TargetID, "flatten": true}, 0)
	if err != nil {
		t.Fatalf("bottest: %v", err)
	}
	var att struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(res, &att)
	st := &pageState{
		b: b, t: t, session: att.SessionID, targetID: target.TargetID,
		loadCh: make(chan struct{}), elementsWait: 5 * time.Second, rec: newRecorder(),
	}
	q := b.registerSession(att.SessionID)
	go st.eventLoop(q)
	for _, c := range []struct {
		method string
		params map[string]interface{}
	}{
		{"Page.enable", nil},
		{"Runtime.enable", nil},
		{"Fetch.enable", map[string]interface{}{"patterns": []map[string]interface{}{{"urlPattern": "*", "requestStage": "Request"}}}},
		// A background tab never "has focus", which suppresses focus events.
		{"Emulation.setFocusEmulationEnabled", map[string]interface{}{"enabled": true}},
	} {
		if _, err := b.send(att.SessionID, c.method, c.params, 0); err != nil {
			t.Fatalf("bottest: %v", err)
		}
	}
	return &Page{st: st}
}

func (st *pageState) eventLoop(q *eventQueue) {
	for {
		ev, ok := q.pop()
		if !ok {
			return
		}
		switch ev.Method {
		case "Fetch.requestPaused":
			go st.handleRequestPaused(ev.Params)
		case "Page.loadEventFired":
			st.mu.Lock()
			st.loads++
			close(st.loadCh)
			st.loadCh = make(chan struct{})
			st.mu.Unlock()
		}
	}
}

func (p *Page) effectiveTimeout() time.Duration {
	if p.timeout > 0 {
		return p.timeout
	}
	return defaultTimeout
}

// Strict returns a view of the same tab whose Eval reports errors (CSP
// blocks, exceptions, {__monoagent_error}) instead of swallowing them the way
// ExtensionPage.Eval does.
func (p *Page) Strict() *Page {
	return &Page{st: p.st, timeout: p.timeout, strict: true}
}

// SetElementsWait changes how long Elements polls for a non-empty result
// (ExtensionPage: 5s). Zero means a single query.
func (p *Page) SetElementsWait(d time.Duration) {
	p.st.mu.Lock()
	p.st.elementsWait = d
	p.st.mu.Unlock()
}

// CDP sends one raw DevTools command to this tab and returns its result.
func (p *Page) CDP(method string, params map[string]interface{}) (map[string]interface{}, error) {
	raw, err := p.st.b.send(p.st.session, method, params, p.effectiveTimeout())
	if err != nil {
		return nil, fmt.Errorf("cdp %s: %w", method, err)
	}
	out := map[string]interface{}{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, nil
}

type evalResult struct {
	Result struct {
		Type  string          `json:"type"`
		Value json.RawMessage `json:"value"`
	} `json:"result"`
	ExceptionDetails *struct {
		Text      string `json:"text"`
		Exception *struct {
			Description string `json:"description"`
		} `json:"exception"`
	} `json:"exceptionDetails"`
}

// evaluate runs Runtime.evaluate (awaiting promises, by value) and returns
// the decoded value. unsafeEvalAllowed=false makes eval/new Function inside
// the expression obey the page's CSP.
func (st *pageState) evaluate(expr string, unsafeEvalAllowed bool, timeout time.Duration) (interface{}, error) {
	params := map[string]interface{}{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	}
	if !unsafeEvalAllowed {
		params["allowUnsafeEvalBlockedByCSP"] = false
	}
	raw, err := st.b.send(st.session, "Runtime.evaluate", params, timeout)
	if err != nil {
		return nil, err
	}
	var res evalResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if d := res.ExceptionDetails; d != nil {
		msg := d.Text
		if d.Exception != nil && d.Exception.Description != "" {
			msg = d.Exception.Description
			if i := strings.IndexByte(msg, '\n'); i > 0 {
				msg = msg[:i]
			}
		}
		return nil, errors.New(msg)
	}
	if len(res.Result.Value) == 0 {
		return nil, nil
	}
	var v interface{}
	if err := json.Unmarshal(res.Result.Value, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// rt calls one function of the injected content-script runtime.
func (p *Page) rt(fn string, params map[string]interface{}) (interface{}, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	expr := "(() => { const B = window.__bottest || (window.__bottest = " + runtimeJS + "); return B." + fn + "(" + string(b) + "); })()"
	return p.st.evaluate(expr, true, 2*time.Minute)
}

// ---------------------------------------------------------------------------
// Navigation
// ---------------------------------------------------------------------------

// Navigate loads url and waits for its load event (like the extension's
// navigate, which waits for the tab to complete). An unserved URL fails
// with net::ERR_BLOCKED_BY_CLIENT.
func (p *Page) Navigate(url string) error {
	p.st.mu.Lock()
	before := p.st.loads
	p.st.mu.Unlock()
	res, err := p.CDP("Page.navigate", map[string]interface{}{"url": url})
	if err != nil {
		return err
	}
	if et, _ := res["errorText"].(string); et != "" {
		return fmt.Errorf("navigate %s: %s", url, et)
	}
	if lid, _ := res["loaderId"].(string); lid == "" {
		return nil // same-document navigation: no load event
	}
	return p.waitLoadAfter(before, p.effectiveTimeout())
}

func (p *Page) waitLoadAfter(n int, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		p.st.mu.Lock()
		loads, ch := p.st.loads, p.st.loadCh
		p.st.mu.Unlock()
		if loads > n {
			return nil
		}
		select {
		case <-ch:
		case <-deadline:
			return fmt.Errorf("page did not finish loading within %v", timeout)
		}
	}
}

// WaitLoad waits until document.readyState is "complete".
func (p *Page) WaitLoad() error {
	deadline := time.Now().Add(p.effectiveTimeout())
	for {
		v, err := p.st.evaluate(`document.readyState`, true, 5*time.Second)
		if err == nil && v == "complete" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("page did not finish loading within %v", p.effectiveTimeout())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// WaitDOMStable behaves like WaitLoad, as on ExtensionPage (the extension
// ignores the mode).
func (p *Page) WaitDOMStable(timeout time.Duration) error { return p.Timeout(timeout).WaitLoad() }

// WaitIdle behaves like WaitLoad, as on ExtensionPage.
func (p *Page) WaitIdle(timeout time.Duration) error { return p.Timeout(timeout).WaitLoad() }

// Reload reloads and waits for the load event.
func (p *Page) Reload() error {
	p.st.mu.Lock()
	before := p.st.loads
	p.st.mu.Unlock()
	if _, err := p.CDP("Page.reload", nil); err != nil {
		return err
	}
	return p.waitLoadAfter(before, p.effectiveTimeout())
}

// GetURL returns the document's current URL.
func (p *Page) GetURL() (string, error) {
	v, err := p.st.evaluate(`location.href`, true, 10*time.Second)
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return s, nil
}

// ---------------------------------------------------------------------------
// Element queries
// ---------------------------------------------------------------------------

func (p *Page) elem(id string) *Element { return &Element{p: p, id: id} }

// poll runs fn every interval until it reports done or timeout passes (at
// least once). Errors — e.g. the execution context being replaced by a
// navigation — count as "not yet".
func poll(timeout, interval time.Duration, fn func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		done, err := fn()
		if err == nil && done {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		if !time.Now().Add(interval).Before(deadline) {
			return lastErr
		}
		time.Sleep(interval)
	}
}

func (p *Page) find(params map[string]interface{}, what string, timeout time.Duration) (browser.ElementHandle, error) {
	var id string
	_ = poll(timeout, 200*time.Millisecond, func() (bool, error) {
		v, err := p.rt("find", params)
		if err != nil {
			return false, err
		}
		id, _ = v.(string)
		return id != "", nil
	})
	if id == "" {
		return nil, fmt.Errorf("Element not found within %dms: %s", timeout.Milliseconds(), what)
	}
	return p.elem(id), nil
}

// Element polls every 200ms for selector until timeout.
func (p *Page) Element(selector string, timeout time.Duration) (browser.ElementHandle, error) {
	return p.find(map[string]interface{}{"selector": selector}, selector, timeout)
}

// ElementX polls every 200ms for xpath until timeout.
func (p *Page) ElementX(xpath string, timeout time.Duration) (browser.ElementHandle, error) {
	return p.find(map[string]interface{}{"xpath": xpath}, xpath, timeout)
}

// Elements returns every match, polling (200ms) up to the elements wait
// (5s by default) for at least one; no match is an empty slice, not an error.
func (p *Page) Elements(selector string) ([]browser.ElementHandle, error) {
	p.st.mu.Lock()
	wait := p.st.elementsWait
	p.st.mu.Unlock()
	var ids []interface{}
	err := poll(wait, 200*time.Millisecond, func() (bool, error) {
		v, err := p.rt("findAll", map[string]interface{}{"selector": selector})
		if err != nil {
			return false, err
		}
		ids, _ = v.([]interface{})
		return len(ids) > 0, nil
	})
	if err != nil && len(ids) == 0 && strings.Contains(err.Error(), "SyntaxError") {
		return nil, err
	}
	out := make([]browser.ElementHandle, 0, len(ids))
	for _, raw := range ids {
		if id, ok := raw.(string); ok {
			out = append(out, p.elem(id))
		}
	}
	return out, nil
}

// Has reports whether selector matches right now.
func (p *Page) Has(selector string) (bool, error) {
	v, err := p.rt("has", map[string]interface{}{"selector": selector})
	if err != nil {
		return false, err
	}
	b, _ := v.(bool)
	return b, nil
}

// Race waits up to timeout for any selector; when several match, the
// earliest in the list wins. Timing out is an error, as on ExtensionPage.
func (p *Page) Race(selectors []string, timeout time.Duration) (int, browser.ElementHandle, error) {
	if len(selectors) == 0 {
		return -1, nil, errors.New("selectors array is required and must be non-empty")
	}
	idx, id := -1, ""
	_ = poll(timeout, 100*time.Millisecond, func() (bool, error) {
		v, err := p.rt("race", map[string]interface{}{"selectors": selectors})
		if err != nil {
			return false, err
		}
		m, _ := v.(map[string]interface{})
		if m == nil {
			return false, nil
		}
		f, _ := m["index"].(float64)
		idx = int(f)
		id, _ = m["id"].(string)
		return id != "", nil
	})
	if id == "" {
		return -1, nil, fmt.Errorf("None of the selectors matched within %dms", timeout.Milliseconds())
	}
	return idx, p.elem(id), nil
}

// Timeout returns a view of the same tab with operations bounded by d.
func (p *Page) Timeout(d time.Duration) browser.PageInterface {
	return &Page{st: p.st, timeout: d, strict: p.strict}
}

// Close closes the tab.
func (p *Page) Close() error {
	_, err := p.st.b.send("", "Target.closeTarget", map[string]interface{}{"targetId": p.st.targetID}, 0)
	return err
}
