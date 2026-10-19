package hilsend

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// Padding can't push a change out of sight: raw JSON text, or an object,
// lists each top-level key with its own cut value, so a key after a padded
// one still shows; past showKeysMax the rest are named.
func TestShowTextListsEveryKeyOfAPaddedObject(t *testing.T) {
	pad := strings.Repeat("A", 600)
	for name, c := range map[string]struct {
		config map[string]interface{}
		want   []string
	}{
		"a padded description before a privacy change": {
			map[string]interface{}{"raw": `{"description":"` + pad + `","is_private":true}`},
			[]string{"\nraw:\n  \"description\": \"" + strings.Repeat("A", showKeyMax-1) + "…\n  \"is_private\": true"}},
		"a padded key before a recipient": {
			map[string]interface{}{"raw": `{"pad":"` + pad + `","to":"evil@x.test"}`},
			[]string{"\n  \"to\": \"evil@x.test\""}},
		"an object option, padded": {
			map[string]interface{}{"raw": map[string]interface{}{"body": pad, "to": "evil@x.test"}},
			[]string{"\n  \"body\": \"AAA", "…\n  \"to\": \"evil@x.test\""}},
		"more keys than shown": {
			map[string]interface{}{"raw": manyKeys(showKeysMax + 3)},
			[]string{"\n  \"k00\": 0", "\n  \"k19\": 19\n  +3 more keys: \"k20\", \"k21\", \"k22\""}},
	} {
		got := showText("op x/y", []string{"raw"}, nil, nil, false, c.config)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: card text\n%s\nlacks %q", name, got, w)
			}
		}
	}
	if got := showText("op x/y", []string{"raw"}, nil, nil, false, map[string]interface{}{"raw": "not {json"}); got != "op x/y with:\nraw: \"not {json\"" {
		t.Errorf("a string that isn't an object: %q", got)
	}
}

// A list shows each element, an object element its keys: a padded first
// article can't hide the second one's recipient, raw JSON or a collection
// list alike; past showElemsMax, how many more.
func TestShowTextListsEveryElementOfAPaddedList(t *testing.T) {
	pad := strings.Repeat("A", 600)
	second := "\n  [1]:\n    \"body\": \"x\"\n    \"to\": \"evil@x.test\""
	for name, val := range map[string]interface{}{
		"raw JSON": `[{"body":"` + pad + `","type":"email"},{"body":"x","to":"evil@x.test"}]`,
		"a collection list": []interface{}{map[string]interface{}{"body": pad, "type": "email"},
			map[string]interface{}{"body": "x", "to": "evil@x.test"}},
	} {
		got := showText("zammad ticket/update", []string{"updateFields.articles"}, nil, nil, true,
			map[string]interface{}{"updateFields": map[string]interface{}{"articles": val}})
		if !strings.Contains(got, "\nupdateFields.articles:\n  [0]:\n    \"body\": \"AAA") || !strings.Contains(got, second) {
			t.Errorf("%s: card text\n%s\nlacks the second article's recipient", name, got)
		}
	}
	var many []interface{}
	for i := 0; i < showElemsMax+2; i++ {
		many = append(many, pad)
	}
	got := showText("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": many})
	if !strings.Contains(got, "\n  [9]: \"AAA") || !strings.HasSuffix(got, "…\n  +2 more elements") || strings.Contains(got, "[10]") {
		t.Errorf("many elements: %q", got)
	}
}

func manyKeys(n int) map[string]interface{} {
	m := map[string]interface{}{}
	for i := 0; i < n; i++ {
		m[fmt.Sprintf("k%02d", i)] = i
	}
	return m
}

// The characters that read as nothing or change how text reads show as
// \uXXXX, and a cut lands on a rune boundary: the card is valid UTF-8.
func TestShowTextEscapesHiddenCharactersAndCutsOnARune(t *testing.T) {
	for in, want := range map[string]string{
		"pay\u202Eto":                   "\"pay\\u202Eto\"",
		"a\u200Bb":                      "\"a\\u200Bb\"",
		"x\U000E0041y":                  "\"x\\u{E0041}y\"",
		"no\u00A0break":                 "\"no\\u00A0break\"",
		"line\u2028sep":                 "\"line\\u2028sep\"",
		"sel\uFE0Fector":                "\"sel\\uFE0Fector\"",
		"private\uE000use":              "\"private\\uE000use\"",
		"plain text, \u00E9 and \u65E5": "\"plain text, \u00E9 and \u65E5\"",
	} {
		if got := showText("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": in}); got != "op x/y with:\nv: "+want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	for _, v := range []interface{}{strings.Repeat("é", 400), map[string]interface{}{"k": strings.Repeat("日", 100)}} {
		got := showText("op x/y", []string{"v"}, nil, nil, false, map[string]interface{}{"v": v})
		if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) || !strings.Contains(got, "…") {
			t.Errorf("cut %T: %q", v, got)
		}
	}
}
