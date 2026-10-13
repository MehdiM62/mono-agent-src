package action

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/monoes/mono-agent/internal/browser"
)

// xpathAttrPattern matches XPath expressions ending with /@attribute, used
// to extract an attribute directly from an XPath result.
var xpathAttrPattern = regexp.MustCompile(`^(.+)/@([a-zA-Z_][\w-]*)$`)

// toFloat64Ok extracts a float64 from an interface{} value (as produced by JSON
// unmarshaling or variable resolution). Returns (0, false) when the value is
// nil or cannot be converted.
func toFloat64Ok(v interface{}) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// ---------------------------------------------------------------------------
// Element resolution helper
// ---------------------------------------------------------------------------

// resolveElement attempts to locate an element for the given step using
// several strategies in order of priority:
//  1. ElementRef — retrieve a previously stored element from the execution context.
//  2. Selector — find by CSS selector (with alternatives via FindElementWithAlternatives).
//  3. XPath — find by XPath expression.
//  4. ConfigKey — dynamically obtain a selector from the config manager.
//
// When no element is found, the returned error carries the underlying lookup
// cause (if any) so callers can surface why resolution failed.
func (ae *ActionExecutor) resolveElement(step StepDef) (browser.ElementHandle, error) {
	timeout := stepTimeout(step, 10)

	// 1. ElementRef from context.
	if step.ElementRef != "" {
		elem := ae.execCtx.GetElement(step.ElementRef)
		if elem != nil {
			return elem, nil
		}
		// Also check step results for elements.
		sr := ae.execCtx.GetStepResult(step.ElementRef)
		if sr != nil && sr.Element != nil {
			return sr.Element, nil
		}
		return nil, fmt.Errorf("element ref %q not found in context", step.ElementRef)
	}

	// 2. Selector, falling back to the step's alternatives (on any driver).
	if step.Selector != "" {
		el, err := findWithAlternatives(ae.page, step.Selector, step.Alternatives, timeout)
		if err == nil {
			return el, nil
		}
		return ae.jevResolve(step, err)
	}

	// 3. XPath.
	if step.XPath != "" {
		el, err := ae.page.ElementX(step.XPath, timeout)
		if err != nil {
			return ae.jevResolve(step, err)
		}
		return el, nil
	}

	// 4. ConfigKey — the package's selectors.json first, else ask the
	// config manager for a selector.
	if step.ConfigKey != "" {
		if entry, ok := ae.pkgSelectorEntry(step.ConfigKey); ok {
			return ae.resolvePkgElement(step, entry, timeout)
		}
		configSelector := ae.resolveConfigSelector(step.ConfigKey)
		if configSelector != "" {
			var el browser.ElementHandle
			var err error
			if isXPath(configSelector) {
				el, err = ae.page.ElementX(configSelector, timeout)
			} else {
				el, err = ae.page.Element(configSelector, timeout)
			}
			if err == nil {
				return el, nil
			}
			return nil, err
		}
		return nil, fmt.Errorf("config key %q resolved to no selector", step.ConfigKey)
	}

	return nil, nil
}

// unwrapRodPage extracts the underlying *rod.Page from a browser.PageInterface,
// returning nil if the page is not a RodPage wrapper.
func unwrapRodPage(p browser.PageInterface) *rod.Page {
	if rp, ok := p.(*browser.RodPage); ok {
		return rp.UnwrapRodPage()
	}
	return nil
}

// unwrapRodElement extracts the underlying *rod.Element from a browser.ElementHandle,
// returning nil if the element is not a RodElement wrapper.
func unwrapRodElement(el browser.ElementHandle) *rod.Element {
	if re, ok := el.(*browser.RodElement); ok {
		return re.UnwrapRodElement()
	}
	return nil
}

// stepTimeout returns the step's explicit timeout or the given default, in
// seconds, converted to a time.Duration.
func stepTimeout(step StepDef, defaultSeconds float64) time.Duration {
	if step.Timeout > 0 {
		return time.Duration(step.Timeout * float64(time.Second))
	}
	return time.Duration(defaultSeconds * float64(time.Second))
}

// ---------------------------------------------------------------------------
// 1. stepNavigate — Navigate to a URL
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepNavigate(ctx context.Context, step StepDef) (*StepResult, error) {
	targetURL := step.URL
	if targetURL == "" {
		return &StepResult{Success: false, StepID: step.ID, Error: fmt.Errorf("navigate step %s: empty URL", step.ID)}, nil
	}

	// Resolve relative URLs against the current page URL.
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		if ae.execCtx.CurrentURL != "" {
			base, err := url.Parse(ae.execCtx.CurrentURL)
			if err == nil {
				ref, err := url.Parse(targetURL)
				if err == nil {
					targetURL = base.ResolveReference(ref).String()
				}
			}
		}
		// If still relative, prepend https://
		if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
			targetURL = "https://" + strings.TrimPrefix(targetURL, "/")
		}
	}

	if err := ae.CheckURLAllowed(targetURL); err != nil {
		return &StepResult{Success: false, StepID: step.ID, Error: fmt.Errorf("navigate step %s: %w", step.ID, err)}, nil
	}

	timeout := stepTimeout(step, 30)

	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("url", ae.redact(targetURL)).
		Dur("timeout", timeout).
		Msg("navigating")

	timedPage := ae.page.Timeout(timeout)
	if err := timedPage.Navigate(targetURL); err != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("navigate to %s: %w", targetURL, err),
		}, nil
	}

	// Wait strategy.
	switch step.WaitFor {
	case "dom_ready":
		_ = ae.page.WaitDOMStable(timeout)
	case "network_idle":
		_ = ae.page.WaitIdle(timeout)
	default: // "page_load" or empty
		_ = ae.page.WaitLoad()
	}

	// Update current URL in context.
	currentURL, urlErr := ae.page.GetURL()
	if urlErr == nil && currentURL != "" {
		ae.execCtx.mu.Lock()
		ae.execCtx.CurrentURL = currentURL
		ae.execCtx.mu.Unlock()
		ae.execCtx.SetVariable("current_url", currentURL)
	}

	return &StepResult{Success: true, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 2. stepWait — Wait for element or time duration
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepWait(ctx context.Context, step StepDef) (*StepResult, error) {
	timeout := stepTimeout(step, 10)

	// If ConfigKey is set, obtain the selector from the config manager.
	// A package selector (selectors.json) is tried first.
	// Its lookup (candidates, legacy lookup, the step's selector and
	// alternatives) spends the step timeout once: a match is present, so
	// the wait below returns at once; no match fails the step.
	selector := step.Selector
	if _, ok := ae.pkgSelectorEntry(step.ConfigKey); ok {
		sel, err := ae.selectorWithFallbacks(step, timeout, step.Selector)
		if err != nil {
			return &StepResult{Success: false, StepID: step.ID, Error: fmt.Errorf("wait step %s: %w", step.ID, err)}, nil
		}
		selector = sel
	} else if step.ConfigKey != "" && ae.configMgr != nil {
		configSelector := ae.legacyConfigSelector(step.ConfigKey)
		if configSelector != "" {
			selector = configSelector
		}
	}

	// If we have a selector or XPath, wait for the element.
	if selector != "" {
		ae.logger.Debug().
			Str("stepID", step.ID).
			Str("selector", selector).
			Dur("timeout", timeout).
			Msg("waiting for element")

		var elem browser.ElementHandle
		var findErr error

		if len(step.Alternatives) > 0 {
			elem, findErr = findWithAlternatives(ae.page, selector, step.Alternatives, timeout)
		} else if isXPath(selector) {
			elem, findErr = ae.page.ElementX(selector, timeout)
		} else {
			elem, findErr = ae.page.Element(selector, timeout)
		}

		if findErr != nil {
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   fmt.Errorf("wait for element %s: %w", selector, findErr),
			}, nil
		}

		// Store element if a variable name is provided.
		if step.Variable != "" && elem != nil {
			ae.execCtx.SetElement(step.Variable, elem)
		}

		return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
	}

	if step.XPath != "" {
		ae.logger.Debug().
			Str("stepID", step.ID).
			Str("xpath", step.XPath).
			Dur("timeout", timeout).
			Msg("waiting for element (xpath)")

		var elem browser.ElementHandle
		var findErr error
		if len(step.Alternatives) > 0 {
			elem, findErr = findWithAlternatives(ae.page, step.XPath, step.Alternatives, timeout)
		} else {
			elem, findErr = ae.page.ElementX(step.XPath, timeout)
		}
		if findErr != nil {
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   fmt.Errorf("wait for xpath element %s: %w", step.XPath, findErr),
			}, nil
		}

		if step.Variable != "" && elem != nil {
			ae.execCtx.SetElement(step.Variable, elem)
		}

		return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
	}

	// No selector — just wait for the specified duration.
	// An explicit duration of 0 (or less) means no wait; only a missing
	// duration falls back to the step timeout. Before, "duration": 0 — e.g. a
	// delay input set to 0 — waited the full 10s default.
	duration := timeout
	if d, ok := toFloat64Ok(step.Duration); ok {
		duration = 0
		if d > 0 {
			duration = time.Duration(d * float64(time.Second))
		}
	}

	ae.logger.Debug().
		Str("stepID", step.ID).
		Dur("duration", duration).
		Msg("waiting (time)")

	select {
	case <-ctx.Done():
		return &StepResult{Success: false, StepID: step.ID, Error: ctx.Err()}, nil
	case <-time.After(duration):
	}

	return &StepResult{Success: true, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 3. stepRefresh — Reload the current page
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepRefresh(ctx context.Context, step StepDef) (*StepResult, error) {
	timeout := stepTimeout(step, 30)

	ae.logger.Debug().Str("stepID", step.ID).Msg("refreshing page")

	if err := ae.page.Timeout(timeout).Reload(); err != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("refresh: %w", err),
		}, nil
	}
	_ = ae.page.WaitLoad()

	return &StepResult{Success: true, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 4. stepFindElement — Find an element using CSS selector, XPath, or config
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepFindElement(ctx context.Context, step StepDef) (*StepResult, error) {
	defer ae.releaseJevMarker()
	timeout := stepTimeout(step, 10)

	// A primary selector falls back to its alternatives on any page driver.
	if step.Selector != "" {
		elem, err := findWithAlternatives(ae.page, step.Selector, step.Alternatives, timeout)
		if err != nil {
			elem, err = ae.jevResolve(step, fmt.Errorf("find_element %s: %w", step.ID, err))
			if err != nil {
				return &StepResult{
					Success: false,
					StepID:  step.ID,
					Error:   err,
				}, nil
			}
		}

		// Store the element for later reference.
		ae.execCtx.SetElement(step.ID, elem)
		if step.Variable != "" {
			ae.execCtx.SetElement(step.Variable, elem)
		}
		if step.VariableName != "" {
			ae.execCtx.SetElement(step.VariableName, elem)
		}

		ae.logger.Debug().Str("stepID", step.ID).Msg("element found via selector")
		return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
	}

	// A package selector (configKey in selectors.json) resolves with its
	// own candidate order and fallbacks.
	if step.XPath == "" {
		if entry, ok := ae.pkgSelectorEntry(step.ConfigKey); ok {
			elem, err := ae.resolvePkgElement(step, entry, timeout)
			if err != nil {
				return &StepResult{Success: false, StepID: step.ID, Error: fmt.Errorf("find_element %s: %w", step.ID, err)}, nil
			}
			ae.storeFoundElement(step, elem)
			return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
		}
	}

	// Build selector list from XPath/ConfigKey/Alternatives.
	selectors := ae.buildSelectorList(step)
	if len(selectors) == 0 {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("find_element step %s: no selectors configured", step.ID),
		}, nil
	}

	var elem browser.ElementHandle
	var lastErr error

	for _, sel := range selectors {
		var el browser.ElementHandle
		var err error
		if isXPath(sel) {
			el, err = ae.page.ElementX(sel, timeout)
		} else {
			el, err = ae.page.Element(sel, timeout)
		}
		if err == nil && el != nil {
			elem = el
			break
		}
		// Keep the first meaningful (non-nil) selector error for reporting.
		if err != nil && lastErr == nil {
			lastErr = err
		}
	}

	if elem == nil {
		// One Jev pick after the whole list failed (not one per alternative).
		var err error
		elem, err = ae.jevResolve(step, fmt.Errorf("find_element %s: no matching element found (tried %d selectors): %v", step.ID, len(selectors), lastErr))
		if err != nil {
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   err,
			}, nil
		}
	}

	// Store the element for later reference.
	ae.execCtx.SetElement(step.ID, elem)
	if step.Variable != "" {
		ae.execCtx.SetElement(step.Variable, elem)
	}
	if step.VariableName != "" {
		ae.execCtx.SetElement(step.VariableName, elem)
		// Also set a variable so {{variable_name}} resolves to "true" in templates.
		ae.execCtx.SetVariable(step.VariableName, true)
	}

	ae.logger.Debug().Str("stepID", step.ID).Msg("element found")
	return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
}
