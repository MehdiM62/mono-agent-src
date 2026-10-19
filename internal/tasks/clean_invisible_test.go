package tasks

import (
	"errors"
	"strings"
	"testing"
)

// A title that shows nothing is refused as empty (#868): the zero-width space
// and the byte order mark are dropped from every text, and the invisible
// characters a script or an emoji needs are kept in text but don't make a
// title on their own.
func TestATitleOfInvisibleCharactersOnlyIsEmpty(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"zero-width space U+200B", "\u200b"},
		{"byte order mark U+FEFF", "\ufeff"},
		{"zero-width non-joiner U+200C", "\u200c"},
		{"zero-width joiner U+200D", "\u200d"},
		{"word joiner U+2060", "\u2060"},
		{"Mongolian vowel separator U+180E", "\u180e"},
		{"left-to-right mark U+200E", "\u200e"},
		{"right-to-left mark U+200F", "\u200f"},
		{"Arabic letter mark U+061C", "\u061c"},
		{"several, with spaces", " \u200b \u200d\u2060 \ufeff\u180e "},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanTitle(c.in); got != "" {
				t.Errorf("cleanTitle(%q) = %q, want empty", c.in, got)
			}
			for _, in := range [][3]string{{c.in, "", ""}, {"", "", c.in}} {
				if _, _, err := deriveTitleNotes(in[0], in[1], in[2]); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "needs a title or some text") {
					t.Errorf("deriveTitleNotes(title %q, text %q): err %v, want it refused as empty", in[0], in[2], err)
				}
			}
		})
	}
}

// The zero-width space is dropped wherever it sits, and refused in a URL like
// the other hidden characters.
func TestTheZeroWidthSpaceIsDropped(t *testing.T) {
	if got := cleanText("pay\u200bpal"); got != "paypal" {
		t.Errorf("cleanText = %q, want the zero-width space dropped", got)
	}
	if got := cleanTitle("Fix\u200b it"); got != "Fix it" {
		t.Errorf("cleanTitle = %q", got)
	}
	if got := cleanURL("https://pay\u200bpal.example/"); got != "" {
		t.Errorf("cleanURL kept a URL with a zero-width space: %q", got)
	}
}

// An emoji sequence keeps its joiners: a family emoji title is stored as given.
func TestAnEmojiFamilyTitleIsKeptIntact(t *testing.T) {
	// The family emoji: man, ZWJ, woman, ZWJ, girl.
	const family = "\U0001F468\u200d\U0001F469\u200d\U0001F467"
	for _, in := range []string{family, "Plan the " + family + " trip"} {
		if got := cleanTitle(in); got != in {
			t.Errorf("cleanTitle(%q) = %q, want it intact", in, got)
		}
		title, _, err := deriveTitleNotes(in, "", "")
		if err != nil || title != in {
			t.Errorf("deriveTitleNotes(%q) = %q, %v", in, title, err)
		}
		title, _, err = deriveTitleNotes("", "", in)
		if err != nil || title != in {
			t.Errorf("deriveTitleNotes(text %q) = %q, %v", in, title, err)
		}
	}
}

// A text whose first line shows nothing takes its title from the first line
// that does.
func TestATextTitleSkipsLinesThatShowNothing(t *testing.T) {
	title, notes, err := deriveTitleNotes("", "", "\u200d\u2060\nReply to Sam\nabout the invoice")
	if err != nil || title != "Reply to Sam" || !strings.Contains(notes, "about the invoice") {
		t.Errorf("got (%q, %q, %v)", title, notes, err)
	}
}
