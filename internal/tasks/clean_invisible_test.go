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
		{"Hangul filler U+3164", "\u3164"},
		{"halfwidth Hangul filler U+FFA0", "\uffa0"},
		{"Hangul jungseong filler U+1160", "\u1160"},
		{"Hangul choseong filler U+115F", "\u115f"},
		{"Braille blank U+2800", "\u2800"},
		{"soft hyphen U+00AD", "\u00ad"},
		{"combining grapheme joiner U+034F", "\u034f"},
		{"invisible times U+2062", "\u2062"},
		{"object replacement character U+FFFC", "\ufffc"},
		{"a lone variation selector U+FE0F", "\ufe0f"},
		{"a lone combining acute accent U+0301", "\u0301"},
		{"fillers and marks, with spaces", "\u3164 \u2800\u00ad \ufe0f\u0301"},
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

// A title with something to show keeps the characters that draw nothing,
// as given: they only decide whether a title is empty.
func TestATitleWithTextKeepsItsInvisibleCharacters(t *testing.T) {
	for _, in := range []string{
		"Hi \u3164", "\uffa0Report", "Plan\u00adning", "Cafe\u0301", "\u2764\ufe0f launch", "x\u2062y",
		"See \ufffc", "Look \u2800 here", "a\u034fb", "\u1160\u115fNotes",
	} {
		if got := cleanTitle(in); got != in {
			t.Errorf("cleanTitle(%q) = %q, want it as given", in, got)
		}
		if title, _, err := deriveTitleNotes(in, "", ""); err != nil || title != in {
			t.Errorf("deriveTitleNotes(%q) = %q, %v", in, title, err)
		}
	}
}

// The zero-width space is dropped from a title and refused in a URL, but text
// keeps it: Thai, Khmer and Lao write no spaces between words, and U+200B is
// their word separator.
func TestTheZeroWidthSpaceIsDroppedFromATitleOnly(t *testing.T) {
	if got := cleanTitle("Fix\u200b it"); got != "Fix it" {
		t.Errorf("cleanTitle = %q", got)
	}
	if got := cleanTitle("pay\u200bpal"); got != "paypal" {
		t.Errorf("cleanTitle = %q, want the zero-width space dropped", got)
	}
	if got := cleanText("pay\u200bpal"); got != "pay\u200bpal" {
		t.Errorf("cleanText = %q, want the zero-width space kept", got)
	}
	if got := cleanURL("https://pay\u200bpal.example/"); got != "" {
		t.Errorf("cleanURL kept a URL with a zero-width space: %q", got)
	}
}

// Thai with U+200B between its words: notes, a text's notes, an edit and a
// comment are stored byte for byte; a title drops the separators.
func TestThaiTextKeepsItsWordSeparators(t *testing.T) {
	const thai = "\u0e2a\u0e27\u0e31\u0e2a\u0e14\u0e35\u200b\u0e04\u0e23\u0e31\u0e1a\u200b\u0e27\u0e31\u0e19\u0e19\u0e35\u0e49"
	s, _, _ := newTestStore(t)
	task, _, err := s.Add(bg, "default", AddInput{Title: thai, Notes: thai}, human)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.ReplaceAll(thai, "\u200b", ""); task.Title != want {
		t.Errorf("title %q, want %q", task.Title, want)
	}
	if task.Notes != thai {
		t.Errorf("notes %q, want %q byte for byte", task.Notes, thai)
	}
	got, _, err := s.Get(bg, "default", task.ID)
	if err != nil || got.Notes != thai {
		t.Errorf("read back: notes %q, err %v", got.Notes, err)
	}

	fromText, _, err := s.Add(bg, "default", AddInput{Text: "Thai\n" + thai}, human)
	if err != nil || fromText.Notes != "Thai\n"+thai {
		t.Errorf("text: notes %q, err %v", fromText.Notes, err)
	}

	edited := "\u0e01\u0e02\u200b" + thai
	if got, err := s.Edit(bg, "default", task.ID, Edit{Notes: &edited}, human); err != nil || got.Notes != edited {
		t.Errorf("edit: notes %q, err %v", got.Notes, err)
	}

	if _, err := s.Comment(bg, "default", task.ID, thai, human); err != nil {
		t.Fatal(err)
	}
	_, events, err := s.Get(bg, "default", task.ID)
	if err != nil || len(events) == 0 || events[len(events)-1].Note != thai {
		t.Errorf("comment: events %+v, err %v", events, err)
	}
}

// A title of zero-width spaces alone is still refused.
func TestATitleOfZeroWidthSpacesIsRefused(t *testing.T) {
	s, _, _ := newTestStore(t)
	for _, in := range []AddInput{{Title: "\u200b"}, {Title: "\u200b \u200b", Notes: "n"}, {Text: "\u200b"}} {
		if _, _, err := s.Add(bg, "default", in, human); !errors.Is(err, ErrInvalid) {
			t.Errorf("Add(%+v): err %v, want it refused", in, err)
		}
	}
	task := mustAdd(t, s, "default", "t", false)
	zw := "\u200b"
	if _, err := s.Edit(bg, "default", task.ID, Edit{Title: &zw}, human); !errors.Is(err, ErrInvalid) {
		t.Errorf("Edit to a zero-width title: err %v, want it refused", err)
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

// Notes that show nothing are stored empty, by an edit as by an add.
func TestNotesThatShowNothingAreStoredEmpty(t *testing.T) {
	s, _, _ := newTestStore(t)
	for _, notes := range []string{"‍", " ‍⁠ ", "ㅤ⠀"} {
		added, _, err := s.Add(bg, "default", AddInput{Title: "t", Notes: notes}, human)
		if err != nil || added.Notes != "" {
			t.Errorf("Add notes %q: stored %q, err %v", notes, added.Notes, err)
		}
		task, _, err := s.Add(bg, "default", AddInput{Title: "t", Notes: "something"}, human)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.Edit(bg, "default", task.ID, Edit{Notes: &notes}, human)
		if err != nil || got.Notes != "" {
			t.Errorf("Edit notes %q: stored %q, err %v", notes, got.Notes, err)
		}
	}
}
