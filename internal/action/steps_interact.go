package action

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/bot"
	"github.com/monoes/mono-agent/internal/fsconfine"
	"github.com/monoes/mono-agent/internal/util"
)

// ---------------------------------------------------------------------------
// 5. stepClick — Click on an element
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepClick(ctx context.Context, step StepDef) (*StepResult, error) {
	defer ae.releaseJevMarker()
	elem, resolveErr := ae.resolveElement(step)
	if elem == nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   noElementError("click step %s: no element to click", step.ID, resolveErr),
		}, nil
	}

	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("waitFor", step.WaitFor).
		Str("waitAfter", step.WaitAfter).
		Msg("clicking element")

	// Only use ClickAndWaitNavigation for explicit page-level navigation waits.
	if step.WaitFor == "page_load" || step.WaitFor == "navigation" {
		rodPage := unwrapRodPage(ae.page)
		rodElem := unwrapRodElement(elem)
		if rodPage != nil && rodElem != nil {
			if err := bot.ClickAndWaitNavigation(rodPage, rodElem); err != nil {
				return &StepResult{
					Success: false,
					StepID:  step.ID,
					Error:   fmt.Errorf("click %s (with navigation wait): %w", step.ID, err),
				}, nil
			}
		} else {
			// Fallback: click + wait load.
			if err := elem.Click(); err != nil {
				return &StepResult{
					Success: false,
					StepID:  step.ID,
					Error:   fmt.Errorf("click %s: %w", step.ID, err),
				}, nil
			}
			_ = ae.page.WaitLoad()
		}
		return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
	}

	// Standard click.
	if err := elem.Click(); err != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("click %s: %w", step.ID, err),
		}, nil
	}

	// Post-click wait strategy — uses WaitFor if WaitAfter is empty.
	waitStep := step
	if step.WaitAfter == "" && step.WaitFor != "" {
		waitStep.WaitAfter = step.WaitFor
	}
	ae.waitAfterClick(waitStep)

	return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
}

// waitAfterClick handles different wait strategies after a click action.
func (ae *ActionExecutor) waitAfterClick(step StepDef) {
	waitStrategy := step.WaitAfter

	switch waitStrategy {
	case "navigation", "page_load":
		_ = ae.page.WaitLoad()

	case "ajax", "network_idle":
		_ = ae.page.WaitIdle(10 * time.Second)

	case "race":
		// Wait for any of the race selectors to appear.
		if len(step.RaceSelectors) > 0 {
			timeout := stepTimeout(step, 5)
			label, err := waitForOutcomes(ae.page, step.RaceSelectors, timeout)
			ae.logger.Debug().Str("stepID", step.ID).Str("outcome", label).Err(err).Msg("race wait after click")
		}

	case "":
		// Default: short wait for UI to settle.
		util.SleepRandom(300*time.Millisecond, 700*time.Millisecond)

	default:
		// Treat as a CSS selector to wait for.
		el, err := ae.page.Element(waitStrategy, 5*time.Second)
		if err == nil && el != nil {
			_ = el.WaitStable(500 * time.Millisecond)
		}
	}
}

// ---------------------------------------------------------------------------
// 6. stepType — Type text into an input element
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepType(ctx context.Context, step StepDef) (*StepResult, error) {
	defer ae.releaseJevMarker()
	elem, resolveErr := ae.resolveElement(step)
	if elem == nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   noElementError("type step %s: no element to type into", step.ID, resolveErr),
		}, nil
	}

	// Determine the text to type. Prefer step.Value (string), fall back to step.Text.
	text := step.Text
	if text == "" {
		if s, ok := step.Value.(string); ok {
			text = s
		} else if step.Value != nil {
			text = fmt.Sprintf("%v", step.Value)
		}
	}

	ae.logger.Debug().
		Str("stepID", step.ID).
		Bool("humanLike", step.HumanLike).
		Int("textLen", len(text)).
		Msg("typing text")
	before, _ := readFieldValue(elem)

	if step.HumanLike {
		// Type with randomized pacing for session stability (no typo simulation).
		// Pass the Rod page so keystrokes use page.Keyboard (not el.Type) — this
		// ensures text reaches the focused element even if the DOM node was
		// swapped on focus (e.g. Instagram replaces textarea with contenteditable).
		rodPage := unwrapRodPage(ae.page)
		rodElem := unwrapRodElement(elem)
		if rodPage != nil && rodElem != nil {
			if err := bot.WriteHumanLike(rodPage, rodElem, text); err != nil {
				return &StepResult{
					Success: false,
					StepID:  step.ID,
					Error:   fmt.Errorf("type (human-like) %s: %w", step.ID, err),
				}, nil
			}
		} else {
			// Extension path: type via CDP with real click + Input.insertText.
			if ep, ok := ae.page.(cdpTyper); ok {
				if ee, ok := elem.(elementIDer); ok {
					// CDP click + insert — real browser events, works with Lexical
					_ = elem.Focus() // TypeCDPOnElement types into the focused element
					if err := ep.TypeCDPOnElement(text, ee.ElementID()); err != nil {
						if err2 := ae.page.InsertText(text); err2 != nil {
							return &StepResult{Success: false, StepID: step.ID, Error: fmt.Errorf("type %s: %w", step.ID, err)}, nil
						}
					}
				} else {
					_ = elem.Click()
					time.Sleep(500 * time.Millisecond)
					if err := ep.TypeCDP(text); err != nil {
						_ = ae.page.InsertText(text)
					}
				}
			} else {
				_ = elem.Click()
				time.Sleep(200 * time.Millisecond)
				if err := ae.page.InsertText(text); err != nil {
					_ = elem.Input(text)
				}
			}
		}
	} else {
		if ep, ok := ae.page.(cdpTyper); ok {
			if ee, ok := elem.(elementIDer); ok {
				_ = elem.Focus() // TypeCDPOnElement types into the focused element
				if err := ep.TypeCDPOnElement(text, ee.ElementID()); err != nil {
					_ = ae.page.InsertText(text)
				}
			} else {
				_ = elem.Click()
				time.Sleep(200 * time.Millisecond)
				if err := ep.TypeCDP(text); err != nil {
					_ = ae.page.InsertText(text)
				}
			}
		} else {
			_ = elem.Click()
			time.Sleep(200 * time.Millisecond)
			if err := ae.page.InsertText(text); err != nil {
				_ = elem.Input(text)
			}
		}
	}

	// Drivers can report success while typing nothing: read the field back.
	if failed := ae.ensureTyped(step, elem, before, text); failed != nil {
		return failed, nil
	}
	return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 6b. stepUpload — Set files on a file input element
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepUpload(ctx context.Context, step StepDef) (*StepResult, error) {
	defer ae.releaseJevMarker()
	// C-46: uploaded files leave the machine through the page; in a run a
	// role's grant started they must come from the role's workdir. Checked
	// before the page is touched.
	rawFiles := splitUploadPaths(step)
	files, err := fsconfine.Paths(ctx, rawFiles)
	if err == nil {
		err = ae.confineUploads(ctx, rawFiles, files) // package actions: §8 C1
	}
	if err != nil {
		return &StepResult{Success: false, StepID: step.ID, Error: fmt.Errorf("upload step %s: %w", step.ID, err)}, nil
	}

	elem, resolveErr := ae.resolveElement(step)
	if elem == nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   noElementError("upload step %s: no file input element found", step.ID, resolveErr),
		}, nil
	}

	if len(files) == 0 {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("upload step %s: no file path provided", step.ID),
		}, nil
	}

	ae.logger.Debug().
		Str("stepID", step.ID).
		Strs("files", files).
		Msg("uploading files")

	// Rod's SetFiles sets file paths on an <input type="file"> element.
	if err := elem.SetFiles(files); err != nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   fmt.Errorf("upload %s: %w", step.ID, err),
		}, nil
	}

	return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 7. stepScroll — Scroll page or element
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepScroll(ctx context.Context, step StepDef) (*StepResult, error) {
	ae.logger.Debug().
		Str("stepID", step.ID).
		Str("direction", step.Direction).
		Interface("duration", step.Duration).
		Msg("scrolling")

	// If an element reference is given, scroll it into view.
	if step.ElementRef != "" {
		elem := ae.execCtx.GetElement(step.ElementRef)
		if elem != nil {
			if err := elem.ScrollIntoView(); err != nil {
				return &StepResult{
					Success: false,
					StepID:  step.ID,
					Error:   fmt.Errorf("scroll element into view %s: %w", step.ID, err),
				}, nil
			}

			// Additional scroll within the element for list containers.
			_ = ae.page.MouseScroll(0, 300, 3)

			waitDuration := time.Duration(1 * time.Second)
			if d, ok := toFloat64Ok(step.Duration); ok && d > 0 {
				waitDuration = time.Duration(d * float64(time.Second))
			}
			util.SleepRandom(waitDuration, waitDuration+500*time.Millisecond)

			return &StepResult{Success: true, StepID: step.ID}, nil
		}
	}

	// Page-level scroll.
	direction := strings.ToLower(step.Direction)
	if direction == "" {
		direction = "down"
	}

	scrollY := 300.0
	if direction == "up" {
		scrollY = -300.0
	}

	duration, ok := toFloat64Ok(step.Duration)
	if !ok || duration <= 0 {
		duration = 1.0
	}

	steps := int(duration * 3) // approximately 3 scroll increments per second
	if steps < 1 {
		steps = 1
	}

	for i := 0; i < steps; i++ {
		select {
		case <-ctx.Done():
			return &StepResult{Success: false, StepID: step.ID, Error: ctx.Err()}, nil
		default:
		}

		err := ae.page.MouseScroll(0, float64(scrollY)/float64(steps), 1)
		if err != nil {
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   fmt.Errorf("scroll %s: %w", step.ID, err),
			}, nil
		}

		util.SleepRandom(200*time.Millisecond, 400*time.Millisecond)
	}

	return &StepResult{Success: true, StepID: step.ID}, nil
}

// ---------------------------------------------------------------------------
// 8. stepHover — Hover over an element
// ---------------------------------------------------------------------------

func (ae *ActionExecutor) stepHover(ctx context.Context, step StepDef) (*StepResult, error) {
	defer ae.releaseJevMarker()
	elem, resolveErr := ae.resolveElement(step)
	if elem == nil {
		return &StepResult{
			Success: false,
			StepID:  step.ID,
			Error:   noElementError("hover step %s: no element to hover", step.ID, resolveErr),
		}, nil
	}

	ae.logger.Debug().Str("stepID", step.ID).Msg("hovering over element")

	// Hover requires scrolling into view first, then focusing.
	// For Rod elements, use the native Hover method via unwrap.
	rodElem := unwrapRodElement(elem)
	if rodElem != nil {
		if err := rodElem.Hover(); err != nil {
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   fmt.Errorf("hover %s: %w", step.ID, err),
			}, nil
		}
	} else {
		// Fallback: scroll into view + focus.
		if err := elem.ScrollIntoView(); err != nil {
			return &StepResult{
				Success: false,
				StepID:  step.ID,
				Error:   fmt.Errorf("hover %s (scroll): %w", step.ID, err),
			}, nil
		}
	}

	return &StepResult{Success: true, Element: elem, StepID: step.ID}, nil
}
