package stylebundle

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Tokens are a page's design tokens as it renders them.
type Tokens struct {
	URL         string            `json:"url"`
	Viewport    Viewport          `json:"viewport"`
	Palette     []PaletteColor    `json:"palette"`
	Variables   map[string]string `json:"variables"`
	Fonts       []FontUse         `json:"fonts"`
	TypeScale   []TypeStep        `json:"typeScale"`
	Spacing     []ValueUse        `json:"spacing"`
	Radii       []ValueUse        `json:"radii"`
	Shadows     []ValueUse        `json:"shadows"`
	Breakpoints []QueryUse        `json:"breakpoints"`
	Notes       []string          `json:"notes,omitempty"`
}

// PaletteColor is one color and its share of what the page paints:
// backgrounds by visible viewport area, text by estimated glyph area,
// borders by stroke area.
type PaletteColor struct {
	Hex   string   `json:"hex,omitempty"`
	Value string   `json:"value"`
	Share float64  `json:"share"`
	Roles []string `json:"roles"`
}

// FontUse is a font stack and how much text uses it.
type FontUse struct {
	Family string  `json:"family"`
	Stack  string  `json:"stack"`
	Chars  int     `json:"chars"`
	Share  float64 `json:"share"`
}

// TypeStep is one font size of the type scale.
type TypeStep struct {
	Size        string   `json:"size"`
	Px          float64  `json:"px"`
	Chars       int      `json:"chars"`
	Weights     []string `json:"weights"`
	LineHeights []string `json:"lineHeights"`
}

// ValueUse is a value and how many times elements use it.
type ValueUse struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// QueryUse is a media query and how many rules it holds.
type QueryUse struct {
	Query    string  `json:"query"`
	MinWidth float64 `json:"minWidth,omitempty"`
	MaxWidth float64 `json:"maxWidth,omitempty"`
	Rules    int     `json:"rules"`
}

// rawTokens is what js/tokens.js returns.
type rawTokens struct {
	URL      string                        `json:"url"`
	Viewport Viewport                      `json:"viewport"`
	Palette  map[string]map[string]float64 `json:"palette"`
	Fonts    map[string]float64            `json:"fonts"`
	Scale    map[string]struct {
		Chars       float64            `json:"chars"`
		Weights     map[string]float64 `json:"weights"`
		LineHeights map[string]float64 `json:"lineHeights"`
	} `json:"scale"`
	Spacing            map[string]float64 `json:"spacing"`
	Radii              map[string]float64 `json:"radii"`
	Shadows            map[string]float64 `json:"shadows"`
	RootVars           map[string]string  `json:"rootVars"`
	Media              map[string]float64 `json:"media"`
	Elements           int                `json:"elements"`
	InaccessibleSheets int                `json:"inaccessibleSheets"`
}

// Caps on what tokens.json lists.
const (
	maxPalette  = 24
	maxValues   = 24
	maxFonts    = 12
	maxScale    = 16
	maxElements = 4000
)

// ReadTokens reads the design tokens of the page p shows.
func ReadTokens(p Page) (*Tokens, error) {
	var raw rawTokens
	if err := evalPage(p, "tokens", tokensJS, map[string]int{
		"maxElements": maxElements, "maxTextNodes": 20000, "maxVars": 500,
	}, &raw); err != nil {
		return nil, err
	}
	return raw.normalize(), nil
}

func (r rawTokens) normalize() *Tokens {
	t := &Tokens{
		URL: originPath(r.URL), Viewport: r.Viewport,
		Palette:     palette(r.Palette),
		Variables:   map[string]string{},
		Fonts:       fontUses(r.Fonts),
		Spacing:     valueUses(r.Spacing, pxOrder),
		Radii:       valueUses(r.Radii, pxOrder),
		Shadows:     valueUses(r.Shadows, nil),
		Breakpoints: queryUses(r.Media),
		TypeScale:   []TypeStep{},
	}
	for k, v := range r.RootVars {
		t.Variables[k] = stripURLQueries(v)
	}
	for size, s := range r.Scale {
		t.TypeScale = append(t.TypeScale, TypeStep{
			Size: size, Px: px(size), Chars: int(s.Chars),
			Weights: ranked(s.Weights), LineHeights: ranked(s.LineHeights),
		})
	}
	sort.Slice(t.TypeScale, func(i, j int) bool {
		if t.TypeScale[i].Px != t.TypeScale[j].Px {
			return t.TypeScale[i].Px > t.TypeScale[j].Px
		}
		return t.TypeScale[i].Size < t.TypeScale[j].Size
	})
	if len(t.TypeScale) > maxScale {
		t.TypeScale = t.TypeScale[:maxScale]
	}
	if r.Elements >= maxElements {
		t.Notes = append(t.Notes, fmt.Sprintf("borders, spacing, radii and shadows count the first %d visible elements", maxElements))
	}
	if r.InaccessibleSheets > 0 {
		t.Notes = append(t.Notes, fmt.Sprintf("%d cross-origin stylesheet(s) could not be read for breakpoints", r.InaccessibleSheets))
	}
	return t
}

// palette merges the per-role areas into one ranking by total area.
func palette(roles map[string]map[string]float64) []PaletteColor {
	byKey := map[string]*PaletteColor{}
	area := map[string]float64{}
	var total float64
	for _, role := range []string{"background", "text", "border"} {
		for value, a := range roles[role] {
			if a <= 0 {
				continue
			}
			hex := cssHex(value)
			key := hex
			if key == "" {
				key = value
			}
			c := byKey[key]
			if c == nil {
				c = &PaletteColor{Hex: hex, Value: value}
				byKey[key] = c
			}
			if len(c.Roles) == 0 || c.Roles[len(c.Roles)-1] != role {
				c.Roles = append(c.Roles, role)
			}
			area[key] += a
			total += a
		}
	}
	out := make([]PaletteColor, 0, len(byKey))
	for key, c := range byKey {
		c.Share = round4(area[key] / total)
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Share != out[j].Share {
			return out[i].Share > out[j].Share
		}
		return out[i].Value < out[j].Value
	})
	if len(out) > maxPalette {
		out = out[:maxPalette]
	}
	return out
}

var rgbFunc = regexp.MustCompile(`^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)\s*(?:[,/]\s*([\d.]+)(%?))?\s*\)$`)

// cssHex turns a computed rgb()/rgba() color into #rrggbb (or #rrggbbaa
// when it is not opaque). Other color spaces stay as written: "".
func cssHex(v string) string {
	m := rgbFunc.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return ""
	}
	var out strings.Builder
	out.WriteByte('#')
	for _, c := range m[1:4] {
		f, _ := strconv.ParseFloat(c, 64)
		fmt.Fprintf(&out, "%02x", int(math.Round(math.Max(0, math.Min(255, f)))))
	}
	if m[4] != "" {
		a, _ := strconv.ParseFloat(m[4], 64)
		if m[5] == "%" {
			a /= 100
		}
		if a < 1 {
			fmt.Fprintf(&out, "%02x", int(math.Round(math.Max(0, a)*255)))
		}
	}
	return out.String()
}

func fontUses(stacks map[string]float64) []FontUse {
	var total float64
	for _, n := range stacks {
		total += n
	}
	out := []FontUse{}
	for stack, n := range stacks {
		first, _, _ := strings.Cut(stack, ",")
		out = append(out, FontUse{Family: unquote(first), Stack: stack, Chars: int(n), Share: round4(n / total)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Chars != out[j].Chars {
			return out[i].Chars > out[j].Chars
		}
		return out[i].Stack < out[j].Stack
	})
	if len(out) > maxFonts {
		out = out[:maxFonts]
	}
	return out
}

// valueUses ranks values by use; then, if given, by less (ascending px).
func valueUses(m map[string]float64, less func(a, b string) bool) []ValueUse {
	out := []ValueUse{}
	for v, n := range m {
		out = append(out, ValueUse{Value: stripURLQueries(v), Count: int(n)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if less != nil && px(out[i].Value) != px(out[j].Value) {
			return less(out[i].Value, out[j].Value)
		}
		return out[i].Value < out[j].Value
	})
	if len(out) > maxValues {
		out = out[:maxValues]
	}
	return out
}

func pxOrder(a, b string) bool { return px(a) < px(b) }

// px reads a leading CSS length in px ("12px", "12.5px 4px" → 12.5); 0 otherwise.
func px(v string) float64 {
	v = strings.TrimSpace(v)
	end := strings.Index(v, "px")
	if end <= 0 {
		return 0
	}
	f, err := strconv.ParseFloat(v[:end], 64)
	if err != nil {
		return 0
	}
	return f
}

func ranked(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// queryUses lists media queries, width breakpoints first in ascending order.
func queryUses(media map[string]float64) []QueryUse {
	out := []QueryUse{}
	for q, n := range media {
		u := QueryUse{Query: q, Rules: int(n)}
		u.MinWidth, u.MaxWidth = queryWidths(q)
		out = append(out, u)
	}
	key := func(u QueryUse) float64 {
		if u.MinWidth > 0 {
			return u.MinWidth
		}
		if u.MaxWidth > 0 {
			return u.MaxWidth
		}
		return math.Inf(1)
	}
	sort.Slice(out, func(i, j int) bool {
		if key(out[i]) != key(out[j]) {
			return key(out[i]) < key(out[j])
		}
		return out[i].Query < out[j].Query
	})
	return out
}

func round4(f float64) float64 { return math.Round(f*10000) / 10000 }
