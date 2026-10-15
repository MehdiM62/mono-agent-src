package stylebundle

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// inherited lists the properties a child takes from its parent unless set,
// as Chrome names them in computed style. Such a property is only worth
// keeping where it differs from the parent's.
var inherited = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`
		accent-color border-collapse border-spacing caption-side caret-color clip-rule color
		color-interpolation color-interpolation-filters color-rendering color-scheme cursor direction
		dominant-baseline dynamic-range-limit empty-cells fill fill-opacity fill-rule font font-family
		font-feature-settings font-kerning font-language-override font-optical-sizing font-palette
		font-size font-size-adjust font-stretch font-style font-synthesis font-synthesis-position
		font-synthesis-small-caps font-synthesis-style font-synthesis-weight font-variant
		font-variant-alternates font-variant-caps font-variant-east-asian font-variant-emoji
		font-variant-ligatures font-variant-numeric font-variant-position font-variation-settings
		font-weight forced-color-adjust hyphenate-character hyphenate-limit-chars hyphens
		image-orientation image-rendering interactivity letter-spacing line-break line-height
		list-style list-style-image list-style-position list-style-type marker marker-end marker-mid
		marker-start math-depth math-shift math-style orphans overflow-wrap paint-order pointer-events
		print-color-adjust quotes ruby-align ruby-position shape-rendering speak stroke
		stroke-dasharray stroke-dashoffset stroke-linecap stroke-linejoin stroke-miterlimit
		stroke-opacity stroke-width tab-size text-align text-align-last text-anchor text-autospace
		text-combine-upright text-decoration-skip-ink text-emphasis-color text-emphasis-position
		text-emphasis-style text-indent text-justify text-orientation text-rendering text-shadow
		text-size-adjust text-spacing-trim text-transform text-underline-offset
		text-underline-position text-wrap text-wrap-mode text-wrap-style visibility white-space
		white-space-collapse widows word-break word-spacing writing-mode
		-webkit-border-horizontal-spacing -webkit-border-vertical-spacing -webkit-font-smoothing
		-webkit-hyphenate-character -webkit-line-break -webkit-locale -webkit-print-color-adjust
		-webkit-rtl-ordering -webkit-tap-highlight-color -webkit-text-combine
		-webkit-text-decorations-in-effect -webkit-text-emphasis-color -webkit-text-emphasis-position
		-webkit-text-emphasis-style -webkit-text-fill-color -webkit-text-orientation
		-webkit-text-security -webkit-text-stroke-color -webkit-text-stroke-width
		-webkit-user-modify -webkit-writing-mode`) {
		inherited[n] = true
	}
}

// logicalProp matches flow-relative properties (margin-inline-start,
// block-size, border-start-end-radius, …). In horizontal left-to-right text
// each one repeats a physical property that is listed anyway.
var logicalProp = regexp.MustCompile(`(^|-)(block|inline)(-|$)|^border-(start|end)-(start|end)-radius$`)

// prune keeps the computed properties the page actually set: an inherited
// property that differs from the parent's, any other property that differs
// from a fresh element of the same tag. Custom properties are left out
// (Bundle.Variables lists the ones in use), and so are empty values, the
// flow-relative duplicates of physical properties, a transform/perspective
// origin at the element's center and colors that only follow the element's
// own color (currentcolor), as they do by default.
func prune(computed, parent, defaults map[string]string, centerOrigin string) map[string]string {
	wm, dir := computed["writing-mode"], computed["direction"]
	physical := (wm == "" || wm == "horizontal-tb") && (dir == "" || dir == "ltr")
	out := map[string]string{}
	for name, v := range computed {
		switch {
		case v == "" || strings.HasPrefix(name, "--"):
			continue
		case physical && logicalProp.MatchString(name):
			continue
		case (name == "transform-origin" || name == "perspective-origin") && centerOrigin != "" && v == centerOrigin:
			continue
		case !inherited[name] && defaults != nil && defaults[name] != "" &&
			defaults[name] == defaults["color"] && v == computed["color"]:
			continue
		case inherited[name] && parent != nil:
			if parent[name] == v {
				continue
			}
		case defaults != nil:
			if d, ok := defaults[name]; ok && d == v {
				continue
			}
		}
		out[name] = stripURLQueries(v)
	}
	return out
}

var (
	cssURL  = regexp.MustCompile(`url\(\s*(['"]?)([^'")]*)(['"]?)\s*\)`)
	bareURL = regexp.MustCompile(`\b(?:https?|wss?|ftp)://[^\s'"()<>]*[^\s'"()<>.,;:!]`)
)

// stripURLQueries drops the query string and fragment of every URL in a CSS
// value or attribute (signed asset URLs carry tokens there) and shortens
// data: URLs to their media type.
func stripURLQueries(s string) string {
	if !strings.Contains(s, "://") && !strings.Contains(s, "url(") && !strings.Contains(s, "data:") {
		return s
	}
	s = cssURL.ReplaceAllStringFunc(s, func(m string) string {
		sub := cssURL.FindStringSubmatch(m)
		return "url(" + sub[1] + cleanURL(sub[2]) + sub[3] + ")"
	})
	return bareURL.ReplaceAllStringFunc(s, cleanURL)
}

func cleanURL(u string) string {
	if strings.HasPrefix(u, "#") {
		return u // a reference inside the document: url(#gradient)
	}
	if strings.HasPrefix(strings.ToLower(u), "data:") {
		head, _, _ := strings.Cut(u, ",")
		return fmt.Sprintf("%s,<%d bytes omitted>", head, len(u)-len(head)-1)
	}
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i]
	}
	return u
}

var (
	widthMinColon = regexp.MustCompile(`(?:^|[^-\w])min-width\s*:\s*([\d.]+)(px|em|rem)`)
	widthMaxColon = regexp.MustCompile(`(?:^|[^-\w])max-width\s*:\s*([\d.]+)(px|em|rem)`)
	widthGE       = regexp.MustCompile(`(?:^|[^-\w])width\s*>=?\s*([\d.]+)(px|em|rem)`)
	widthLE       = regexp.MustCompile(`(?:^|[^-\w])width\s*<=?\s*([\d.]+)(px|em|rem)`)
	widthLEFirst  = regexp.MustCompile(`([\d.]+)(px|em|rem)\s*<=?\s*width\b`)
	widthGEFirst  = regexp.MustCompile(`([\d.]+)(px|em|rem)\s*>=?\s*width\b`)
)

// queryWidths reads the viewport (or container) width bounds of a media
// condition, in px (an em is 16px). Zero means unbounded.
func queryWidths(cond string) (minW, maxW float64) {
	px := func(re *regexp.Regexp) float64 {
		m := re.FindStringSubmatch(cond)
		if m == nil {
			return 0
		}
		f, _ := strconv.ParseFloat(m[1], 64)
		if m[2] != "px" {
			f *= 16
		}
		return f
	}
	minW = max(px(widthMinColon), px(widthGE), px(widthLEFirst))
	for _, v := range []float64{px(widthMaxColon), px(widthLE), px(widthGEFirst)} {
		if v > 0 && (maxW == 0 || v < maxW) {
			maxW = v
		}
	}
	return minW, maxW
}

func withWidths(bps []Breakpoint) []Breakpoint {
	if bps == nil {
		return []Breakpoint{}
	}
	for i := range bps {
		bps[i].MinWidth, bps[i].MaxWidth = queryWidths(bps[i].Condition)
	}
	return bps
}
