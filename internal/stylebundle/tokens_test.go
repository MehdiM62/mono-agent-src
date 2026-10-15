package stylebundle

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCSSHex(t *testing.T) {
	for in, want := range map[string]string{
		"rgb(51, 85, 255)":         "#3355ff",
		"rgba(0, 0, 0, 0.15)":      "#00000026",
		"rgba(255, 255, 255, 1)":   "#ffffff",
		"rgb(10 20 30 / 50%)":      "#0a141e80",
		"rgb(300, -1, 0)":          "",
		"oklch(0.7 0.1 200)":       "",
		"color(srgb 1 0 0)":        "",
		" rgb(1.6, 2.4, 3) ":       "#020203",
		"rgba(12, 34, 56, 0.999)":  "#0c2238ff",
		"transparent":              "",
		"rgb(255, 255, 255, 0.5)x": "",
	} {
		if got := cssHex(in); got != want {
			t.Errorf("cssHex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokensNormalize(t *testing.T) {
	var raw rawTokens
	if err := json.Unmarshal([]byte(`{
	  "url": "https://shop.test/a/b?session=1#x",
	  "viewport": {"width": 1000, "height": 800, "deviceScaleFactor": 2},
	  "palette": {
	    "background": {"rgb(255, 255, 255)": 600, "rgba(255, 255, 255, 1)": 100, "oklch(0.5 0.1 20)": 50},
	    "text": {"rgb(17, 17, 17)": 200, "rgb(255, 255, 255)": 20},
	    "border": {"rgb(221, 221, 221)": 30, "rgb(1, 1, 1)": 0}
	  },
	  "fonts": {"Inter, sans-serif": 900, "\"Fira Code\", monospace": 100},
	  "scale": {
	    "16px": {"chars": 800, "weights": {"400": 700, "600": 100}, "lineHeights": {"24px": 800}},
	    "32px": {"chars": 20, "weights": {"700": 20}, "lineHeights": {"normal": 20}},
	    "12.5px": {"chars": 80, "weights": {"400": 80}, "lineHeights": {"normal": 80}}
	  },
	  "spacing": {"16px": 10, "8px": 10, "24px": 3},
	  "radii": {"4px": 2, "999px": 5},
	  "shadows": {"rgba(0, 0, 0, 0.1) 0px 1px 2px 0px": 4},
	  "rootVars": {"--brand": "#33f", "--hero": "url(https://cdn/h.jpg?sig=9)"},
	  "media": {"(max-width: 600px)": 4, "(min-width: 1200px)": 2, "print": 7, "(min-width: 768px)": 3},
	  "elements": 4000,
	  "inaccessibleSheets": 1
	}`), &raw); err != nil {
		t.Fatal(err)
	}
	tk := raw.normalize()
	if tk.URL != "https://shop.test/a/b" {
		t.Errorf("url %q", tk.URL)
	}
	if len(tk.Palette) != 4 {
		t.Fatalf("palette %+v", tk.Palette)
	}
	white := tk.Palette[0]
	if white.Hex != "#ffffff" || white.Share != 0.72 || strings.Join(white.Roles, ",") != "background,text" {
		t.Errorf("white %+v (of 1000 painted, 600+100+20 is white)", white)
	}
	if tk.Palette[1].Hex != "#111111" || tk.Palette[2].Hex != "" || tk.Palette[2].Value != "oklch(0.5 0.1 20)" {
		t.Errorf("ranking %+v", tk.Palette)
	}
	if tk.Fonts[0].Family != "Inter" || tk.Fonts[0].Share != 0.9 || tk.Fonts[1].Family != "Fira Code" {
		t.Errorf("fonts %+v", tk.Fonts)
	}
	if len(tk.TypeScale) != 3 || tk.TypeScale[0].Px != 32 || tk.TypeScale[2].Px != 12.5 ||
		strings.Join(tk.TypeScale[1].Weights, ",") != "400,600" {
		t.Errorf("type scale %+v", tk.TypeScale)
	}
	if tk.Spacing[0].Value != "8px" || tk.Spacing[1].Value != "16px" || tk.Spacing[2].Value != "24px" {
		t.Errorf("spacing (count, then px) %+v", tk.Spacing)
	}
	if tk.Radii[0].Value != "999px" || len(tk.Shadows) != 1 {
		t.Errorf("radii %+v shadows %+v", tk.Radii, tk.Shadows)
	}
	if tk.Variables["--hero"] != "url(https://cdn/h.jpg)" {
		t.Errorf("vars %v", tk.Variables)
	}
	var qs []string
	for _, b := range tk.Breakpoints {
		qs = append(qs, b.Query)
	}
	if strings.Join(qs, " | ") != "(max-width: 600px) | (min-width: 768px) | (min-width: 1200px) | print" {
		t.Errorf("breakpoints %v", qs)
	}
	if len(tk.Notes) != 2 {
		t.Errorf("notes %v", tk.Notes)
	}
}

func TestTokensFromRecording(t *testing.T) {
	tk, err := ReadTokens(loadReplay(t, replayFile))
	if err != nil {
		t.Fatal(err)
	}
	if tk.Palette[0].Hex != "#f5f6fa" || tk.Variables["--unused"] != "1px" || len(tk.Fonts) == 0 || tk.Fonts[0].Family != "Fixture Serif" {
		t.Errorf("tokens %+v", tk)
	}
	var widths []float64
	for _, b := range tk.Breakpoints {
		widths = append(widths, b.MinWidth+b.MaxWidth)
	}
	if len(widths) != 3 || widths[0] != 600 || widths[1] != 1200 {
		t.Errorf("breakpoints %+v", tk.Breakpoints)
	}
}

func TestSourceFromRecording(t *testing.T) {
	f := loadReplay(t, replayFile)
	for _, tc := range []struct {
		sel, framework, component, file string
		line                            int
		min                             float64
	}{
		{"#card", "", "", "/home/dev/app/src/components/Card.tsx", 12, 0.9},
		{"#react-btn", "react", "SaveButton", "/home/dev/app/src/SaveButton.jsx", 8, 0.9},
		{"#vue-el", "vue", "Hello", "/home/dev/app/src/components/Hello.vue", 0, 0.7},
		{"#svelte-el", "svelte", "", "src/lib/Counter.svelte", 3, 0.85},
		{"#plain-el", "", "", "", 0, 0.1},
	} {
		s, err := ReadSource(f, tc.sel)
		if err != nil {
			t.Fatalf("%s: %v", tc.sel, err)
		}
		if s.Framework != tc.framework || s.Component != tc.component || s.File != tc.file || s.Line != tc.line || s.Confidence < tc.min {
			t.Errorf("%s: %+v", tc.sel, s)
		}
	}
	s, _ := ReadSource(f, "#react-btn")
	if strings.Join(s.Components, ">") != "SaveButton>SettingsPanel" {
		t.Errorf("react owners %v", s.Components)
	}
	s, _ = ReadSource(f, "#plain-el")
	hints := map[Hint]bool{}
	for _, h := range s.Hints {
		hints[h] = true
	}
	for _, h := range []Hint{{"test-id", "price"}, {"class", "price-tag"}, {"text", "$19"}, {"id", "plain-el"}, {"tag", "span"}} {
		if !hints[h] {
			t.Errorf("hint %+v missing from %+v", h, s.Hints)
		}
	}
}

// evalOnly is a Page whose evaluations return a canned value.
type evalOnly struct {
	result interface{}
	err    error
}

func (e evalOnly) CDP(string, map[string]interface{}) (map[string]interface{}, error) {
	return nil, errors.New("no CDP")
}
func (e evalOnly) EvalCDP(string) (interface{}, error) { return e.result, e.err }

func TestDecodeEvalErrors(t *testing.T) {
	var out map[string]any
	for _, tc := range []struct {
		page evalOnly
		want string
	}{
		{evalOnly{err: errors.New("bridge down")}, "source: bridge down"},
		{evalOnly{result: map[string]any{"notFound": true}}, ErrNotFound.Error()},
		{evalOnly{result: map[string]any{"badSelector": "nope"}}, "cannot parse the selector: nope"},
		{evalOnly{result: map[string]any{"error": "TypeError: x"}}, "source: TypeError: x"},
		{evalOnly{result: nil}, "source: the page returned nothing"},
		{evalOnly{result: "a string"}, "source: unexpected result"},
	} {
		err := evalOn(tc.page, "source", "(function(){})", "#x", nil, &out)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: err %v, want %q", tc.page, err, tc.want)
		}
	}
}
