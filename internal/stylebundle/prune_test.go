package stylebundle

import (
	"reflect"
	"testing"
	"time"
)

func TestPrune(t *testing.T) {
	computed := map[string]string{
		"color":             "rgb(34, 34, 34)", // same as the parent: inherited, dropped
		"font-size":         "20px",            // differs from the parent: kept
		"display":           "block",           // the tag's default: dropped
		"width":             "320px",           // not the default: kept
		"margin-top":        "0px",             // default: dropped
		"border-top-color":  "rgb(34, 34, 34)", // follows color: dropped
		"outline-color":     "rgb(255, 0, 0)",  // set to something else: kept
		"inline-size":       "320px",           // logical duplicate of width: dropped
		"margin-block-end":  "8px",             // logical: dropped
		"transform-origin":  "176px 99px",      // the center: dropped
		"--brand":           "#3355ff",         // custom property: listed elsewhere
		"all":               "",                // empty: dropped
		"background-image":  `url("https://cdn/x.png?sig=1")`,
		"writing-mode":      "horizontal-tb",
		"direction":         "ltr",
		"letter-spacing":    "normal", // inherited, parent differs: kept
		"cursor":            "pointer",
		"some-new-property": "x", // unknown to the defaults: kept
	}
	parent := map[string]string{"color": "rgb(34, 34, 34)", "font-size": "16px", "letter-spacing": "1px", "cursor": "auto",
		"writing-mode": "horizontal-tb", "direction": "ltr"}
	defaults := map[string]string{
		"color": "rgb(0, 0, 0)", "display": "block", "width": "0px", "margin-top": "0px",
		"border-top-color": "rgb(0, 0, 0)", "outline-color": "rgb(0, 0, 0)", "background-image": "none",
	}
	got := prune(computed, parent, defaults, "176px 99px")
	want := map[string]string{
		"font-size": "20px", "width": "320px", "outline-color": "rgb(255, 0, 0)",
		"background-image": `url("https://cdn/x.png")`, "letter-spacing": "normal", "cursor": "pointer",
		"some-new-property": "x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("prune:\n got %v\nwant %v", got, want)
	}
}

func TestPruneVerticalTextKeepsLogicalProperties(t *testing.T) {
	got := prune(map[string]string{"writing-mode": "vertical-rl", "inline-size": "10px"}, nil, nil, "")
	if got["inline-size"] != "10px" {
		t.Errorf("vertical text lost inline-size: %v", got)
	}
}

func TestPruneWithoutParentOrDefaults(t *testing.T) {
	// The root element has no parent: an inherited property falls back to the defaults.
	got := prune(map[string]string{"color": "rgb(0, 0, 0)", "font-size": "18px"}, nil,
		map[string]string{"color": "rgb(0, 0, 0)", "font-size": "16px"}, "")
	if !reflect.DeepEqual(got, map[string]string{"font-size": "18px"}) {
		t.Errorf("root: %v", got)
	}
	got = prune(map[string]string{"display": "block"}, nil, nil, "")
	if got["display"] != "block" {
		t.Errorf("no defaults should keep everything: %v", got)
	}
}

func TestStripURLQueries(t *testing.T) {
	for in, want := range map[string]string{
		`url("a.png?sig=1")`:                         `url("a.png")`,
		`url(https://cdn.x/a.png?t=1#frag)`:          `url(https://cdn.x/a.png)`,
		`url('x.woff2?v=2') format("woff2"), url(y)`: `url('x.woff2') format("woff2"), url(y)`,
		`url(#gradient)`:                             `url(#gradient)`,
		`see https://a.b/c?d=e&f=g, then`:            `see https://a.b/c, then`,
		`url("data:image/png;base64,AAAA")`:          `url("data:image/png;base64,<4 bytes omitted>")`,
		`12px solid red`:                             `12px solid red`,
		`wss://h/p?token=1`:                          `wss://h/p`,
	} {
		if got := stripURLQueries(in); got != want {
			t.Errorf("stripURLQueries(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestQueryWidths(t *testing.T) {
	for cond, want := range map[string][2]float64{
		"(max-width: 600px)":                        {0, 600},
		"screen and (min-width: 48em)":              {768, 0},
		"(min-width: 600px) and (max-width: 900px)": {600, 900},
		"(width >= 1024px)":                         {1024, 0},
		"(width < 40rem)":                           {0, 640},
		"(600px <= width <= 900px)":                 {600, 900},
		"(prefers-color-scheme: dark)":              {0, 0},
		"(min-device-width: 500px)":                 {0, 0},
		"(max-height: 500px)":                       {0, 0},
	} {
		minW, maxW := queryWidths(cond)
		if minW != want[0] || maxW != want[1] {
			t.Errorf("queryWidths(%q) = %v,%v want %v", cond, minW, maxW, want)
		}
	}
}

func TestTransitionWait(t *testing.T) {
	for _, tc := range []struct {
		dur, delay string
		want       time.Duration
	}{
		{"0s", "0s", 0},
		{"0.2s, 0.2s", "0s, 0s", 250 * time.Millisecond},
		{"150ms", "100ms", 300 * time.Millisecond},
		{"0.1s, 0.3s", "0s", 350 * time.Millisecond}, // a shorter delay list repeats
		{"5s", "0s", time.Second},
		{"", "", 0},
	} {
		if got := transitionWait(map[string]string{"transition-duration": tc.dur, "transition-delay": tc.delay}); got != tc.want {
			t.Errorf("transitionWait(%q, %q) = %v, want %v", tc.dur, tc.delay, got, tc.want)
		}
	}
}
