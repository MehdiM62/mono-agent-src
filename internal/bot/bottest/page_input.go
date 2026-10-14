package bottest

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-rod/rod/lib/proto"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Keyboard / input (synthetic, like the content script)
// ---------------------------------------------------------------------------

// KeyboardType dispatches synthetic key/input events per character on the
// focused element.
func (p *Page) KeyboardType(keys ...rune) error {
	if len(keys) == 0 {
		return errors.New("text is required")
	}
	_, err := p.rt("keyboardType", map[string]interface{}{"text": string(keys)})
	return err
}

// KeyboardPress dispatches a synthetic keydown/keypress/keyup for key
// (string(key) as the key name, e.g. '\r' — like the extension).
func (p *Page) KeyboardPress(key rune) error {
	_, err := p.rt("keyboardPress", map[string]interface{}{"key": string(key)})
	return err
}

// InsertText inserts text at the focused element (paste → execCommand →
// value → textContent).
func (p *Page) InsertText(text string) error {
	_, err := p.rt("insertText", map[string]interface{}{"text": text})
	return err
}

// InsertTextOnElement focuses and clicks the element, then inserts text.
func (p *Page) InsertTextOnElement(text string, elementID string) error {
	_, err := p.rt("insertText", map[string]interface{}{"text": text, "id": elementID})
	return err
}

// MouseScroll scrolls the window by (x, y) (smooth; steps is ignored, as in
// the extension).
func (p *Page) MouseScroll(x, y float64, steps int) error {
	_, err := p.rt("scrollBy", map[string]interface{}{"x": x, "y": y})
	return err
}

// ---------------------------------------------------------------------------
// Eval
// ---------------------------------------------------------------------------

// Eval runs js like the extension's MAIN-world eval: compiled with
// new Function (blocked by a CSP without 'unsafe-eval'); a function is
// called with args. Errors are swallowed into a nil result exactly like
// ExtensionPage.Eval; use Strict() to get them.
func (p *Page) Eval(js string, args ...interface{}) (*browser.EvalResult, error) {
	if args == nil {
		args = []interface{}{}
	}
	a, err := json.Marshal(args)
	if err != nil {
		if p.strict {
			return nil, err
		}
		return browser.NewEvalResult(nil), nil
	}
	code, _ := json.Marshal(js)
	v, err := p.st.evaluate(fmt.Sprintf(evalWrapperJS, a, code), false, 10*time.Second)
	if err == nil {
		if m, ok := v.(map[string]interface{}); ok {
			if msg, ok := m["__monoagent_error"]; ok {
				err = fmt.Errorf("Eval: %v", msg)
			}
		}
	}
	if err != nil {
		if p.strict {
			return nil, err
		}
		return browser.NewEvalResult(nil), nil
	}
	return browser.NewEvalResult(v), nil
}

// EvalCDP evaluates js via Runtime.evaluate (CSP does not apply), awaiting a
// returned promise; exceptions are errors.
func (p *Page) EvalCDP(js string) (interface{}, error) {
	v, err := p.st.evaluate(js, true, p.effectiveTimeout())
	if err != nil {
		return nil, fmt.Errorf("eval_cdp: %w", err)
	}
	return v, nil
}

// TypeCDP focuses the first visible contenteditable (>100×30) or
// [role=textbox] and inserts text with Input.insertText.
func (p *Page) TypeCDP(text string) error { return p.TypeCDPOnElement(text, "") }

// TypeCDPOnElement mirrors the extension: the element is only used (clicked
// at its centre) if the focus script itself fails; otherwise it types into
// the first visible contenteditable, like TypeCDP.
func (p *Page) TypeCDPOnElement(text string, elementID string) error {
	if text == "" {
		return errors.New("text required")
	}
	v, err := p.st.evaluate(typeCDPFocusJS, true, 10*time.Second)
	if err == nil {
		if m, ok := v.(map[string]interface{}); ok && m["found"] == true {
			time.Sleep(300 * time.Millisecond)
		}
	} else if elementID != "" {
		if r, rerr := p.rt("rect", map[string]interface{}{"id": elementID}); rerr == nil {
			if m, ok := r.(map[string]interface{}); ok {
				x := m["x"].(float64) + m["width"].(float64)/2
				y := m["y"].(float64) + m["height"].(float64)/2
				for _, typ := range []string{"mousePressed", "mouseReleased"} {
					_, _ = p.CDP("Input.dispatchMouseEvent", map[string]interface{}{"type": typ, "x": x, "y": y, "button": "left", "clickCount": 1})
				}
				time.Sleep(300 * time.Millisecond)
			}
		}
	}
	_, err = p.CDP("Input.insertText", map[string]interface{}{"text": text})
	return err
}

// ---------------------------------------------------------------------------
// Cookies
// ---------------------------------------------------------------------------

// SetCookies accepts []*proto.NetworkCookieParam, like ExtensionPage.
func (p *Page) SetCookies(cookies interface{}) error {
	params, ok := cookies.([]*proto.NetworkCookieParam)
	if !ok {
		return fmt.Errorf("SetCookies: expected []*proto.NetworkCookieParam, got %T", cookies)
	}
	if len(params) == 0 {
		return nil
	}
	_, err := p.CDP("Network.setCookies", map[string]interface{}{"cookies": params})
	return err
}

// GetCookies returns the current URL's non-httpOnly cookies as a
// []interface{} of maps, like ExtensionPage.
func (p *Page) GetCookies() (interface{}, error) {
	u, err := p.GetURL()
	if err != nil {
		return nil, err
	}
	res, err := p.CDP("Network.getCookies", map[string]interface{}{"urls": []string{u}})
	if err != nil {
		return nil, err
	}
	all, _ := res["cookies"].([]interface{})
	out := []interface{}{}
	for _, c := range all {
		if m, ok := c.(map[string]interface{}); ok && m["httpOnly"] != true {
			out = append(out, m)
		}
	}
	return out, nil
}
