package stylebundle

import (
	"encoding/base64"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// cdp is the DOM/CSS-domain half of Build.
type cdp struct{ p Page }

func (c cdp) call(method string, params map[string]interface{}) (map[string]interface{}, error) {
	return c.p.CDP(method, params)
}

// node resolves selector to a DOM nodeId, enabling the DOM and CSS domains
// the CSS commands need.
func (c cdp) node(selector string) (float64, error) {
	if _, err := c.call("DOM.enable", nil); err != nil {
		return 0, err
	}
	if _, err := c.call("CSS.enable", nil); err != nil {
		return 0, err
	}
	doc, err := c.call("DOM.getDocument", map[string]interface{}{"depth": 0})
	if err != nil {
		return 0, err
	}
	root, _ := doc["root"].(map[string]interface{})
	rootID, _ := root["nodeId"].(float64)
	q, err := c.call("DOM.querySelector", map[string]interface{}{"nodeId": rootID, "selector": selector})
	if err != nil {
		return 0, err
	}
	id, _ := q["nodeId"].(float64)
	if id == 0 {
		return 0, ErrNotFound
	}
	return id, nil
}

// computed returns every computed property, custom properties included.
func (c cdp) computed(nodeID float64) (map[string]string, error) {
	res, err := c.call("CSS.getComputedStyleForNode", map[string]interface{}{"nodeId": nodeID})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, raw := range list(res["computedStyle"]) {
		m, _ := raw.(map[string]interface{})
		name, _ := m["name"].(string)
		value, _ := m["value"].(string)
		if name != "" {
			out[name] = value
		}
	}
	return out, nil
}

func list(v interface{}) []interface{} {
	l, _ := v.([]interface{})
	return l
}

func str(m map[string]interface{}, k string) string {
	s, _ := m[k].(string)
	return s
}

func obj(m map[string]interface{}, k string) map[string]interface{} {
	o, _ := m[k].(map[string]interface{})
	return o
}

// rulesFrom turns CSS.getMatchedStylesForNode into the element's rules and
// its pseudo-elements' rules. The browser's own (user-agent) rules are left
// out: the pruned computed styles already account for them.
func rulesFrom(matched map[string]interface{}) ([]Rule, map[string][]Rule) {
	var rules []Rule
	if st := obj(matched, "attributesStyle"); st != nil {
		if d := declarations(st); len(d) > 0 {
			rules = append(rules, Rule{Selector: "presentational attributes", Origin: "attributes", Declarations: d})
		}
	}
	rules = append(rules, ruleMatches(list(matched["matchedCSSRules"]))...)
	if st := obj(matched, "inlineStyle"); st != nil {
		if d := declarations(st); len(d) > 0 {
			rules = append(rules, Rule{Selector: "style attribute", Origin: "inline", Declarations: d})
		}
	}
	var pseudo map[string][]Rule
	for _, raw := range list(matched["pseudoElements"]) {
		pm, _ := raw.(map[string]interface{})
		kind := str(pm, "pseudoType")
		if id := str(pm, "pseudoIdentifier"); id != "" {
			kind += "(" + id + ")"
		}
		if r := ruleMatches(list(pm["matches"])); len(r) > 0 {
			if pseudo == nil {
				pseudo = map[string][]Rule{}
			}
			pseudo[kind] = append(pseudo[kind], r...)
		}
	}
	return rules, pseudo
}

func ruleMatches(matches []interface{}) []Rule {
	var out []Rule
	for _, raw := range matches {
		rm, _ := raw.(map[string]interface{})
		rule := obj(rm, "rule")
		origin := str(rule, "origin")
		if rule == nil || origin == "user-agent" {
			continue
		}
		r := Rule{
			Selector:     matchingSelector(obj(rule, "selectorList"), list(rm["matchingSelectors"])),
			Origin:       origin,
			Media:        texts(rule["media"]),
			Supports:     texts(rule["supports"]),
			Container:    texts(rule["containerQueries"]),
			Layers:       texts(rule["layers"]),
			Declarations: declarations(obj(rule, "style")),
		}
		out = append(out, r)
	}
	return out
}

// matchingSelector keeps the selectors of a list that matched the element.
func matchingSelector(sl map[string]interface{}, matching []interface{}) string {
	sels := list(sl["selectors"])
	var parts []string
	for _, i := range matching {
		idx, ok := i.(float64)
		if !ok || int(idx) < 0 || int(idx) >= len(sels) {
			continue
		}
		if s, _ := sels[int(idx)].(map[string]interface{}); s != nil {
			parts = append(parts, str(s, "text"))
		}
	}
	if len(parts) == 0 {
		return str(sl, "text")
	}
	return strings.Join(parts, ", ")
}

func texts(v interface{}) []string {
	var out []string
	for _, raw := range list(v) {
		m, _ := raw.(map[string]interface{})
		t := str(m, "text")
		if name := str(m, "name"); name != "" && t != "" {
			t = name + " " + t
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// declarations lists what a style sets as written: the source-backed
// entries when there are any (the browser repeats them expanded into
// longhands without a source range), else every explicit entry.
func declarations(style map[string]interface{}) []Declaration {
	props := list(style["cssProperties"])
	sourced := false
	for _, raw := range props {
		if m, _ := raw.(map[string]interface{}); m != nil && m["range"] != nil {
			sourced = true
			break
		}
	}
	var out []Declaration
	for _, raw := range props {
		m, _ := raw.(map[string]interface{})
		if m == nil || (sourced && m["range"] == nil) {
			continue
		}
		if b, _ := m["disabled"].(bool); b {
			continue
		}
		if b, _ := m["implicit"].(bool); b {
			continue
		}
		if ok, present := m["parsedOk"].(bool); present && !ok {
			continue
		}
		name, value := str(m, "name"), str(m, "value")
		if name == "" || value == "" {
			continue
		}
		important, _ := m["important"].(bool)
		out = append(out, Declaration{Name: name, Value: stripURLQueries(value), Important: important})
	}
	return out
}

var varRef = regexp.MustCompile(`var\(\s*(--[A-Za-z0-9_-]+)`)

// usedVariables resolves every custom property the rules reference, as the
// element computes it.
func usedVariables(rules []Rule, pseudo map[string][]Rule, computed map[string]string) map[string]string {
	out := map[string]string{}
	add := func(rs []Rule) {
		for _, r := range rs {
			for _, d := range r.Declarations {
				names := varRef.FindAllStringSubmatch(d.Value, -1)
				if strings.HasPrefix(d.Name, "--") {
					names = append(names, []string{"", d.Name})
				}
				for _, m := range names {
					if v, ok := computed[m[1]]; ok {
						out[m[1]] = stripURLQueries(strings.TrimSpace(v))
					}
				}
			}
		}
	}
	add(rules)
	for _, rs := range pseudo {
		add(rs)
	}
	return out
}

// fontFacesFrom reads the @font-face rules CSS.getMatchedStylesForNode
// reports for the element's own font (cssAtRules, current Chrome).
func fontFacesFrom(matched map[string]interface{}) []FontFace {
	var out []FontFace
	for _, raw := range list(matched["cssAtRules"]) {
		m, _ := raw.(map[string]interface{})
		if str(m, "type") != "font-face" {
			continue
		}
		f := FontFace{}
		for _, d := range declarations(obj(m, "style")) {
			switch d.Name {
			case "font-family":
				f.Family = unquote(d.Value)
			case "src":
				f.Src = d.Value
			case "font-weight":
				f.Weight = d.Value
			case "font-style":
				f.Style = d.Value
			case "font-stretch":
				f.Stretch = d.Value
			case "font-display":
				f.Display = d.Value
			case "unicode-range":
				f.UnicodeRange = d.Value
			}
		}
		if f.Family != "" {
			out = append(out, f)
		}
	}
	return out
}

// mergeFontFaces joins the CDP and in-page @font-face lists: one entry per
// family, weight, style and source; a FontFace without a src (from
// document.fonts) only adds what no rule already describes.
func mergeFontFaces(lists ...[]FontFace) []FontFace {
	out := []FontFace{}
	for _, l := range lists {
		for _, f := range l {
			f.Family, f.Src = unquote(f.Family), stripURLQueries(f.Src)
			if f.Weight == "" || f.Weight == "normal" {
				f.Weight = "400"
			}
			if f.Style == "" {
				f.Style = "normal"
			}
			if i := indexFace(out, f); i >= 0 {
				if out[i].Status == "" {
					out[i].Status = f.Status
				}
				continue
			}
			out = append(out, f)
		}
	}
	return out
}

func indexFace(faces []FontFace, f FontFace) int {
	for i, g := range faces {
		if strings.EqualFold(g.Family, f.Family) && g.Weight == f.Weight && g.Style == f.Style && (f.Src == "" || g.Src == f.Src) {
			return i
		}
	}
	return -1
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

func renderedFonts(res map[string]interface{}) []RenderedFont {
	var out []RenderedFont
	for _, raw := range list(res["fonts"]) {
		m, _ := raw.(map[string]interface{})
		custom, _ := m["isCustomFont"].(bool)
		glyphs, _ := m["glyphCount"].(float64)
		out = append(out, RenderedFont{Family: str(m, "familyName"), PostScript: str(m, "postScriptName"), Custom: custom, Glyphs: int(glyphs)})
	}
	return out
}

// maxShotSide caps the screenshot clip (CSS px) so a page-sized element
// cannot produce a frame the bridge refuses.
const maxShotSide = 8000

// screenshot captures the element's box as a PNG, scrolled or not.
func screenshot(c cdp, r Rect) ([]byte, error) {
	if r.Width < 1 || r.Height < 1 {
		return nil, fmt.Errorf("the element has no size (%gx%g)", r.Width, r.Height)
	}
	res, err := c.call("Page.captureScreenshot", map[string]interface{}{
		"format": "png",
		"clip": map[string]interface{}{
			"x": r.X, "y": r.Y,
			"width": math.Min(r.Width, maxShotSide), "height": math.Min(r.Height, maxShotSide),
			"scale": 1,
		},
		"captureBeyondViewport": true,
	})
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(str(res, "data"))
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("no image came back")
	}
	return data, nil
}

// transitionWait is the longest transition (duration + delay) the element
// declares, plus a frame, capped at a second.
func transitionWait(computed map[string]string) time.Duration {
	durs := strings.Split(computed["transition-duration"], ",")
	delays := strings.Split(computed["transition-delay"], ",")
	var longest time.Duration
	for i, d := range durs {
		total := cssTime(d)
		if len(delays) > 0 {
			total += cssTime(delays[i%len(delays)])
		}
		longest = max(longest, total)
	}
	if longest <= 0 {
		return 0
	}
	return min(longest+50*time.Millisecond, time.Second)
}

func cssTime(s string) time.Duration {
	s = strings.TrimSpace(s)
	unit := time.Second
	switch {
	case strings.HasSuffix(s, "ms"):
		s, unit = strings.TrimSuffix(s, "ms"), time.Millisecond
	case strings.HasSuffix(s, "s"):
		s = strings.TrimSuffix(s, "s")
	default:
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0
	}
	return time.Duration(f * float64(unit))
}

// changed lists the properties whose value differs in forced from base.
func changed(base, forced map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range forced {
		if base[k] != v {
			out[k] = stripURLQueries(v)
		}
	}
	return out
}
