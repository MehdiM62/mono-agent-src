package stylebundle

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The in-page readers. Each file is one function expression; evalOn and
// evalPage wrap it so a thrown error comes back as a value (Runtime.evaluate
// reports a throw only as "Uncaught").
var (
	//go:embed js/element.js
	elementJS string
	//go:embed js/tokens.js
	tokensJS string
	//go:embed js/source.js
	sourceJS string
)

var (
	// ErrNotFound means no element matches the selector.
	ErrNotFound = errors.New("no element matches the selector")
	// ErrBadSelector means the browser could not parse the selector.
	ErrBadSelector = errors.New("the browser cannot parse the selector")
)

// evalMarker starts every expression so a recorded call can be matched by
// name rather than by its whole source (see fakePage in the tests).
func evalMarker(name string) string { return "/*stylebundle:" + name + "*/" }

// evalOn runs fn against the first element matching selector and decodes
// its result into out.
func evalOn(p Page, name, fn, selector string, opts any, out any) error {
	sel, err := json.Marshal(selector)
	if err != nil {
		return err
	}
	o, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	expr := evalMarker(name) + `(function () {
  var el;
  try { el = document.querySelector(` + string(sel) + `); } catch (e) { return { badSelector: String(e && e.message || e) }; }
  if (!el) return { notFound: true };
  try { return { ok: (` + strings.TrimSpace(fn) + `)(el, ` + string(o) + `) }; }
  catch (e) { return { error: String(e && e.stack || e) }; }
})()`
	return decodeEval(p, name, expr, out)
}

// evalPage runs fn with opts in the page and decodes its result into out.
func evalPage(p Page, name, fn string, opts any, out any) error {
	o, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	expr := evalMarker(name) + `(function () {
  try { return { ok: (` + strings.TrimSpace(fn) + `)(` + string(o) + `) }; }
  catch (e) { return { error: String(e && e.stack || e) }; }
})()`
	return decodeEval(p, name, expr, out)
}

func decodeEval(p Page, name, expr string, out any) error {
	raw, err := p.EvalCDP(expr)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	var env struct {
		OK          json.RawMessage `json:"ok"`
		Error       string          `json:"error"`
		NotFound    bool            `json:"notFound"`
		BadSelector string          `json:"badSelector"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return fmt.Errorf("%s: unexpected result: %w", name, err)
	}
	switch {
	case env.NotFound:
		return ErrNotFound
	case env.BadSelector != "":
		return fmt.Errorf("%w: %s", ErrBadSelector, env.BadSelector)
	case env.Error != "":
		return fmt.Errorf("%s: %s", name, env.Error)
	case len(env.OK) == 0:
		return fmt.Errorf("%s: the page returned nothing", name)
	}
	if err := json.Unmarshal(env.OK, out); err != nil {
		return fmt.Errorf("%s: unexpected result: %w", name, err)
	}
	return nil
}
