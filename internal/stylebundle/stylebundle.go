// Package stylebundle reads how one element of a live page is styled and
// writes it as a folder a coding agent can rebuild it from: computed styles
// pruned to what the page set, the matched rules, the CSS variables and
// @font-face rules it uses, its hover/focus/active states, the breakpoints
// that touch it, a summary of its children, a sanitized element.html, a
// screenshot, the page's design tokens and where the element comes from in
// the source code.
//
// Everything is read through a Page: raw Chrome DevTools Protocol commands
// (the DOM and CSS domains) plus main-world JavaScript where that is simpler.
// extension.ExtensionPage is one, so it works on the user's real browser
// through the extension bridge. Building a bundle does not change the page,
// apart from forcing pseudo-states for a moment and a hidden frame it adds
// and removes to read the browser's defaults.
//
// A bundle is page-derived data. Its README says so, and nothing in it is
// ever an instruction to whoever reads it.
package stylebundle

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Page is what Build needs from a browser tab.
type Page interface {
	CDP(method string, params map[string]interface{}) (map[string]interface{}, error)
	EvalCDP(js string) (interface{}, error)
}

// DefaultStates are the pseudo-classes Build forces when Options.States is nil.
var DefaultStates = []string{"hover", "focus", "active", "focus-visible"}

// Options tune Build. The zero value is the default.
type Options struct {
	// States are the pseudo-classes to force, one at a time; nil = DefaultStates.
	States []string
	// MaxChildren caps the child summary (default 60).
	MaxChildren int
	// MaxHTMLNodes caps the nodes written to element.html (default 1500).
	MaxHTMLNodes int
}

func (o Options) withDefaults() Options {
	if o.States == nil {
		o.States = DefaultStates
	}
	if o.MaxChildren <= 0 {
		o.MaxChildren = 60
	}
	if o.MaxHTMLNodes <= 0 {
		o.MaxHTMLNodes = 1500
	}
	return o
}

// Rect is a box in document CSS pixels.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Viewport is the tab's layout viewport when the bundle was read.
type Viewport struct {
	Width             float64 `json:"width"`
	Height            float64 `json:"height"`
	DeviceScaleFactor float64 `json:"deviceScaleFactor"`
}

// Declaration is one property a rule sets.
type Declaration struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Important bool   `json:"important,omitempty"`
}

// Rule is one style rule that matches the element, in cascade order.
type Rule struct {
	Selector     string        `json:"selector"`
	Origin       string        `json:"origin"`
	Media        []string      `json:"media,omitempty"`
	Supports     []string      `json:"supports,omitempty"`
	Container    []string      `json:"container,omitempty"`
	Layers       []string      `json:"layers,omitempty"`
	Declarations []Declaration `json:"declarations"`
}

// FontFace is an @font-face rule (or a loaded FontFace) for a used family.
type FontFace struct {
	Family       string `json:"family"`
	Src          string `json:"src,omitempty"`
	Weight       string `json:"weight,omitempty"`
	Style        string `json:"style,omitempty"`
	Stretch      string `json:"stretch,omitempty"`
	Display      string `json:"display,omitempty"`
	UnicodeRange string `json:"unicodeRange,omitempty"`
	Status       string `json:"status,omitempty"`
}

// RenderedFont is a font the browser actually drew the element's text with.
type RenderedFont struct {
	Family     string `json:"family"`
	PostScript string `json:"postScriptName,omitempty"`
	Custom     bool   `json:"custom"`
	Glyphs     int    `json:"glyphs"`
}

// Breakpoint is a media (or container) condition with rules for the
// element or its descendants, whether or not it applies right now.
type Breakpoint struct {
	Type       string   `json:"type"`
	Condition  string   `json:"condition"`
	Active     *bool    `json:"active,omitempty"`
	Target     string   `json:"target"`
	MinWidth   float64  `json:"minWidth,omitempty"`
	MaxWidth   float64  `json:"maxWidth,omitempty"`
	Selectors  []string `json:"selectors"`
	Properties []string `json:"properties"`
}

// Child is one descendant in the child summary.
type Child struct {
	Depth      int      `json:"depth"`
	Tag        string   `json:"tag"`
	ID         string   `json:"id,omitempty"`
	Classes    []string `json:"classes,omitempty"`
	Role       string   `json:"role,omitempty"`
	Display    string   `json:"display"`
	Width      float64  `json:"width"`
	Height     float64  `json:"height"`
	Children   int      `json:"children"`
	TextLength int      `json:"textLength"`
}

// ChildSummary describes the element's descendants, in document order.
type ChildSummary struct {
	Total     int     `json:"total"`
	Direct    int     `json:"direct"`
	Nodes     []Child `json:"nodes"`
	Truncated bool    `json:"truncated"`
}

// Bundle is what Build read. The fields tagged "-" are written as their own
// files by WriteDir.
type Bundle struct {
	URL            string                       `json:"url"`
	Selector       string                       `json:"selector"`
	Tag            string                       `json:"tag"`
	Rect           Rect                         `json:"rect"`
	Viewport       Viewport                     `json:"viewport"`
	CapturedAt     time.Time                    `json:"capturedAt"`
	Computed       map[string]string            `json:"computed"`
	Rules          []Rule                       `json:"rules"`
	PseudoElements map[string][]Rule            `json:"pseudoElements,omitempty"`
	Variables      map[string]string            `json:"variables"`
	FontFaces      []FontFace                   `json:"fontFaces"`
	RenderedFonts  []RenderedFont               `json:"renderedFonts,omitempty"`
	States         map[string]map[string]string `json:"states"`
	Breakpoints    []Breakpoint                 `json:"breakpoints"`
	Children       ChildSummary                 `json:"children"`
	Notes          []string                     `json:"notes,omitempty"`

	HTML       string  `json:"-"`
	Screenshot []byte  `json:"-"`
	Tokens     *Tokens `json:"-"`
	Source     *Source `json:"-"`
}

// elementInfo is what js/element.js returns.
type elementInfo struct {
	URL                string            `json:"url"`
	Tag                string            `json:"tag"`
	Rect               Rect              `json:"rect"`
	Viewport           Viewport          `json:"viewport"`
	CenterOrigin       string            `json:"centerOrigin"`
	Parent             map[string]string `json:"parent"`
	Defaults           map[string]string `json:"defaults"`
	Tree               *htmlNode         `json:"tree"`
	TreeTruncated      bool              `json:"treeTruncated"`
	Children           ChildSummary      `json:"children"`
	Breakpoints        []Breakpoint      `json:"breakpoints"`
	FontFaces          []FontFace        `json:"fontFaces"`
	InaccessibleSheets int               `json:"inaccessibleSheets"`
}

// now and sleep are swapped by tests.
var (
	now   = time.Now
	sleep = time.Sleep
)

// Build reads the element selector matches (the first one) on p.
func Build(p Page, selector string, o Options) (*Bundle, error) {
	o = o.withDefaults()
	if strings.TrimSpace(selector) == "" {
		return nil, fmt.Errorf("%w: empty selector", ErrBadSelector)
	}
	var info elementInfo
	if err := evalOn(p, "element", elementJS, selector, map[string]int{
		"maxChildren": o.MaxChildren, "maxNodes": o.MaxHTMLNodes,
	}, &info); err != nil {
		return nil, err
	}
	b := &Bundle{
		URL: originPath(info.URL), Selector: selector, Tag: info.Tag,
		Rect: info.Rect, Viewport: info.Viewport, CapturedAt: now().UTC(),
		Children:    info.Children,
		Breakpoints: withWidths(info.Breakpoints),
		States:      map[string]map[string]string{},
	}
	if info.InaccessibleSheets > 0 {
		b.note("%d cross-origin stylesheet(s) could not be read for breakpoints and @font-face src", info.InaccessibleSheets)
	}
	if info.Defaults == nil {
		b.note("the browser defaults for <%s> could not be read; computed styles are pruned against the parent only", info.Tag)
	}

	c := cdp{p}
	nodeID, err := c.node(selector)
	if err != nil {
		return nil, err
	}
	computed, err := c.computed(nodeID)
	if err != nil {
		return nil, err
	}
	matched, err := c.call("CSS.getMatchedStylesForNode", map[string]interface{}{"nodeId": nodeID})
	if err != nil {
		return nil, err
	}
	b.Rules, b.PseudoElements = rulesFrom(matched)
	b.Variables = usedVariables(b.Rules, b.PseudoElements, computed)
	b.FontFaces = mergeFontFaces(fontFacesFrom(matched), info.FontFaces)
	if fonts, err := c.call("CSS.getPlatformFontsForNode", map[string]interface{}{"nodeId": nodeID}); err == nil {
		b.RenderedFonts = renderedFonts(fonts)
	}

	if shot, err := screenshot(c, info.Rect); err != nil {
		b.note("element.png: %v", err)
	} else {
		b.Screenshot = shot
	}
	if err := b.readStates(c, nodeID, computed, o.States); err != nil {
		b.note("states: %v", err)
	}

	b.Computed = prune(computed, info.Parent, info.Defaults, info.CenterOrigin)
	b.HTML = renderHTML(info.Tree, info.TreeTruncated)

	if t, err := ReadTokens(p); err != nil {
		b.note("tokens.json: %v", err)
	} else {
		b.Tokens = t
	}
	if s, err := ReadSource(p, selector); err != nil {
		b.note("source.json: %v", err)
	} else {
		b.Source = s
	}
	return b, nil
}

func (b *Bundle) note(format string, a ...any) {
	b.Notes = append(b.Notes, fmt.Sprintf(format, a...))
}

// readStates forces each pseudo-class in turn and records the computed
// properties it changes. Transitions are waited out (up to a second), so a
// hover color is read where it lands, not halfway there.
func (b *Bundle) readStates(c cdp, nodeID float64, base map[string]string, states []string) error {
	if len(states) == 0 {
		return nil
	}
	defer c.call("CSS.forcePseudoState", map[string]interface{}{"nodeId": nodeID, "forcedPseudoClasses": []string{}})
	wait := transitionWait(base)
	var errs []error
	for _, st := range states {
		if _, err := c.call("CSS.forcePseudoState", map[string]interface{}{
			"nodeId": nodeID, "forcedPseudoClasses": []string{st},
		}); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", st, err))
			continue
		}
		if wait > 0 {
			sleep(wait)
		}
		forced, err := c.computed(nodeID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", st, err))
			continue
		}
		b.States[st] = changed(base, forced)
	}
	return errors.Join(errs...)
}

// originPath keeps only the scheme, host and path of a page URL.
func originPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u.String()
}
