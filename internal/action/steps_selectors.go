package action

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// noElementError builds the "no element" error for a step, appending the
// resolution cause when one is available.
func noElementError(msg, stepID string, cause error) error {
	if cause != nil {
		return fmt.Errorf(msg+": %w", stepID, cause)
	}
	return fmt.Errorf(msg, stepID)
}

// buildSelectorList assembles the list of selectors to try for element lookup,
// starting with the primary selector from xpath/selector/configKey, then
// alternatives.
func (ae *ActionExecutor) buildSelectorList(step StepDef) []string {
	var selectors []string

	// Primary selector.
	if step.XPath != "" {
		selectors = append(selectors, step.XPath)
	} else if step.Selector != "" {
		selectors = append(selectors, step.Selector)
	} else if step.ConfigKey != "" {
		// ConfigKey means we should look up the selector from the config manager.
		configSelector := ae.resolveConfigSelector(step.ConfigKey)
		if configSelector != "" {
			selectors = append(selectors, configSelector)
		}
	}

	// Alternatives.
	for _, alt := range step.Alternatives {
		if alt != "" {
			// If the alternative is a configKey (no / or [ prefix), resolve it.
			if !isXPath(alt) && !strings.HasPrefix(alt, ".") && !strings.HasPrefix(alt, "#") {
				resolved := ae.resolveConfigSelector(alt)
				if resolved != "" {
					selectors = append(selectors, resolved)
					continue
				}
			}
			selectors = append(selectors, alt)
		}
	}

	return selectors
}

// resolveConfigSelector resolves a config key to an XPath or CSS selector:
// through the package's selectors.json when it defines the key, else
// through the config manager.
func (ae *ActionExecutor) resolveConfigSelector(configKey string) string {
	if entry, ok := ae.pkgSelectorEntry(configKey); ok {
		if sel := ae.resolvePkgSelectorString(configKey, entry, defaultPkgSelectorTimeout); sel != "" {
			return sel
		}
	}
	return ae.legacyConfigSelector(configKey)
}

// legacyConfigSelector looks up a config key through the config manager to
// get an XPath or CSS selector.
func (ae *ActionExecutor) legacyConfigSelector(configKey string) string {
	if ae.configMgr == nil {
		return ""
	}

	platform := ""
	if ae.action != nil {
		platform = ae.action.TargetPlatform
	}

	configContext := ""
	if v, ok := ae.execCtx.GetVariable("configContext"); ok {
		if s, ok := v.(string); ok {
			configContext = s
		}
	}

	// Get page HTML for config resolution. Prefer CDP evaluation: sites
	// such as LinkedIn and Hacker News forbid 'unsafe-eval', which blocks the
	// extension's plain Eval and left html empty (selectors never resolved).
	var html string
	if cdp, ok := ae.page.(interface {
		EvalCDP(js string) (interface{}, error)
	}); ok {
		if v, err := cdp.EvalCDP(`document.documentElement.outerHTML`); err == nil {
			html, _ = v.(string)
		}
	}
	if html == "" {
		evalResult, evalErr := ae.page.Eval(`() => document.documentElement.outerHTML`)
		if evalErr == nil && evalResult != nil {
			html = evalResult.Str()
		}
	}

	configResult, err := ae.configMgr.GetConfig(
		platform,
		configKey,
		configContext,
		html,
		fmt.Sprintf("Find selector for %s", configKey),
		nil,
	)
	if err != nil {
		ae.logger.Debug().
			Err(err).
			Str("configKey", configKey).
			Msg("config selector resolution failed")
		return ""
	}

	switch v := configResult.(type) {
	case string:
		return v
	case map[string]interface{}:
		if xpath, ok := v["xpath"].(string); ok {
			return xpath
		}
	}
	return ""
}

// resolveRelativeURL converts a relative URL to an absolute one using the
// current page URL as the base.
func (ae *ActionExecutor) resolveRelativeURL(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}

	baseURL := ae.execCtx.CurrentURL
	if baseURL == "" {
		u, err := ae.page.GetURL()
		if err == nil {
			baseURL = u
		}
	}

	if baseURL == "" {
		return rawURL
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return rawURL
	}

	ref, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	return base.ResolveReference(ref).String()
}

// resolveXPathAttribute splits an XPath like "//div/@class" into the base
// XPath "//div" and the attribute name "class".
func resolveXPathAttribute(xpath string) (basePath, attrName string, isAttr bool) {
	matches := xpathAttrPattern.FindStringSubmatch(xpath)
	if len(matches) == 3 {
		return matches[1], matches[2], true
	}
	return xpath, "", false
}

// isXPath returns true if the selector string looks like an XPath expression.
func isXPath(selector string) bool {
	return strings.HasPrefix(selector, "/") ||
		strings.HasPrefix(selector, "(") ||
		strings.HasPrefix(selector, ".//")
}

// unquote removes surrounding single or double quotes from a string.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// toFloat converts a string to a float64, returning 0 on failure.
func toFloat(s string) float64 {
	s = strings.TrimSpace(s)
	s = unquote(s)
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// truncateStr shortens a string for display purposes.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// containsExtractedItem reports whether candidate is already present in items,
// comparing by deep equality (maps are not comparable with ==).
func containsExtractedItem(items []map[string]interface{}, candidate map[string]interface{}) bool {
	for _, item := range items {
		if reflect.DeepEqual(item, candidate) {
			return true
		}
	}
	return false
}
