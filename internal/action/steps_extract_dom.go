package action

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// 9. stepExtractText — Extract text content from an element
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepExtractText(ctx context.Context, step StepDef) (*StepResult, error) {
	timeout := stepTimeout(step, 10)

	selectors := ae.buildSelectorList(step)
	if len(selectors) == 0 {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("extract_text step %s: no selectors configured", step.ID),
		}, nil
	}

	var text string
	var elem browser.ElementHandle
	var lastErr error

	for _, sel := range selectors {
		// Check for XPath attribute pattern: //path/@attribute
		basePath, attrName, isAttr := resolveXPathAttribute(sel)
		if isAttr {
			el, err := ae.page.ElementX(basePath, timeout)
			if err == nil && el != nil {
				attrVal, _ := el.Attribute(attrName)
				if attrVal != nil {
					text = *attrVal
					elem = el
				}
			} else {
				lastErr = err
			}
			if text != "" {
				break
			}
			continue
		}

		var el browser.ElementHandle
		var err error
		if isXPath(sel) {
			el, err = ae.page.ElementX(sel, timeout)
		} else {
			el, err = ae.page.Element(sel, timeout)
		}
		if err == nil && el != nil {
			t, tErr := el.Text()
			if tErr == nil {
				text = t
				elem = el
				break
			}
			lastErr = tErr
		} else {
			lastErr = err
		}
	}

	if text == "" && lastErr != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("extract_text %s: failed to extract text: %v", step.ID, lastErr),
		}, nil
	}

	text = strings.TrimSpace(text)

	// Store in variable.
	varName := step.Variable
	if varName == "" {
		varName = step.VariableName
	}
	if varName != "" {
		ae.execCtx.SetVariable(varName, text)
	}
	ae.execCtx.SetVariable(step.ID, text)

	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("text", truncateStr(text, 80)).
		Msg("text extracted")

	return &StepResult{
		Success: true,
		Data:    text,
		Element: elem,
		StepID:  step.ID,
	}, nil
}

// ---------------------------------------------------------------------------
// 10. stepExtractAttribute — Extract an attribute value from an element
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepExtractAttribute(ctx context.Context, step StepDef) (*StepResult, error) {
	timeout := stepTimeout(step, 10)

	attrName := step.Attribute
	if attrName == "" {
		attrName = "href"
	}

	selectors := ae.buildSelectorList(step)
	if len(selectors) == 0 {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("extract_attribute step %s: no selectors configured", step.ID),
		}, nil
	}

	var attrValue string
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
			val, _ := el.Attribute(attrName)
			if val != nil {
				attrValue = *val
				elem = el
			}
		} else {
			lastErr = err
		}
		if attrValue != "" {
			break
		}
	}

	if attrValue == "" && lastErr != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("extract_attribute %s: attribute %q not found: %v", step.ID, attrName, lastErr),
		}, nil
	}

	// Resolve relative URLs for href/src attributes.
	if (attrName == "href" || attrName == "src") && attrValue != "" {
		attrValue = ae.resolveRelativeURL(attrValue)
	}

	// Store result.
	varName := step.Variable
	if varName == "" {
		varName = step.VariableName
	}
	if varName != "" {
		ae.execCtx.SetVariable(varName, attrValue)
	}
	ae.execCtx.SetVariable(step.ID, attrValue)

	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("attribute", attrName).
		Str("value", truncateStr(attrValue, 100)).
		Msg("attribute extracted")

	return &StepResult{
		Success: true,
		Data:    attrValue,
		Element: elem,
		StepID:  step.ID,
	}, nil
}

// ---------------------------------------------------------------------------
// 11. stepExtractMultiple — Extract data from multiple matching elements
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepExtractMultiple(ctx context.Context, step StepDef) (*StepResult, error) {
	timeout := stepTimeout(step, 15)

	selectors := ae.buildSelectorList(step)
	if len(selectors) == 0 {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("extract_multiple step %s: no selectors configured", step.ID),
		}, nil
	}

	var elements []browser.ElementHandle
	var lastErr, unsupportedErr error

	for _, sel := range selectors {
		var elems []browser.ElementHandle
		var err error
		if isXPath(sel) {
			// PageInterface.Elements only supports CSS; elementsByXPath works
			// on Rod and on any page exposing EvalCDP, and errors otherwise.
			elems, err = elementsByXPath(ae.page, sel, timeout)
			if errors.Is(err, errXPathMultiUnsupported) && unsupportedErr == nil {
				unsupportedErr = err
			}
		} else {
			elems, err = ae.page.Elements(sel)
		}
		if err == nil && len(elems) > 0 {
			elements = elems
			break
		}
		lastErr = err
		elements = nil
	}

	// A driver that cannot evaluate an XPath selector must not turn into a
	// silent zero-item success.
	if len(elements) == 0 && lastErr == nil && unsupportedErr != nil {
		lastErr = unsupportedErr
	}
	if len(elements) == 0 && lastErr != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("extract_multiple %s: no elements found: %w", step.ID, lastErr),
		}, nil
	}

	// Extract values from elements as a slice of maps.
	attrName := step.Attribute
	seen := make(map[string]bool)
	var extracted []map[string]interface{}

	for _, elem := range elements {
		item := make(map[string]interface{})

		// Extract text content.
		text, _ := elem.Text()
		text = strings.TrimSpace(text)
		if text != "" {
			item["text"] = text
		}

		// Extract specified attribute.
		if attrName != "" {
			attrVal, attrErr := elem.Attribute(attrName)
			if attrErr == nil && attrVal != nil {
				val := *attrVal
				// Resolve relative URLs.
				if attrName == "href" || attrName == "src" {
					val = ae.resolveRelativeURL(val)
				}
				item[attrName] = val
			}
		} else {
			// Extract href by default if it exists on this element.
			href, hrefErr := elem.Attribute("href")
			if hrefErr == nil && href != nil && *href != "" {
				item["href"] = ae.resolveRelativeURL(*href)
			} else {
				// Fallback: look for first child <a> with an href (profile link in card containers).
				if childHref, ok := firstChildHref(elem); ok && childHref != "" {
					item["href"] = ae.resolveRelativeURL(childHref)
				}
			}
		}

		// Deduplicate based on the primary extracted value.
		dedupeKey := fmt.Sprintf("%v", item)
		if seen[dedupeKey] {
			continue
		}
		seen[dedupeKey] = true

		extracted = append(extracted, item)
	}

	count := len(extracted)

	ae.logger.Debug().
		Str("stepID", step.ID).
		Int("count", count).
		Msg("multiple elements extracted")

	// Store results.
	varName := step.Variable
	if varName == "" {
		varName = step.ID
	}
	ae.execCtx.SetVariable(varName, extracted)

	// Also add as extracted items for later saving; each is a record.
	for _, item := range extracted {
		ae.execCtx.AddRecord(item)
	}

	result := &StepResult{
		Success: true,
		Data:    extracted,
		StepID:  step.ID,
	}
	ae.execCtx.SetStepResult(step.ID, result)
	ae.execCtx.SetVariable(step.ID+".count", count)
	ae.execCtx.SetVariable("resultsCount", len(ae.execCtx.ExtractedItems))

	return result, nil
}
