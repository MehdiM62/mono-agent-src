package profileinfo

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAIFillWritesOnlyValidAnswersAndNeverTheUsersOrTheCard(t *testing.T) {
	db, _ := newDB(t)
	if err := Set(bg, db, "p1", "first_name", "Typed", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	var prompt string
	complete := func(_ context.Context, p string) (string, error) {
		prompt = p
		return "```json\n" + `{"values":{
			"first_name":{"value":"Ada","fact":1},
			"last_name":{"value":"Lovelace","fact":1},
			"title":{"value":"Countess","fact":1},
			"gender":{"value":"female","fact":1},
			"date_of_birth":{"value":"1815-12-10","fact":2},
			"passport_number":{"value":"X1234567","fact":2},
			"card_number":{"value":"4242424242424242","fact":2},
			"portrait_photo":{"value":"me.png","fact":1},
			"nonsense":{"value":"x","fact":1}}}` + "\n```", nil
	}
	facts := []Fact{{Source: "/docs/cv.pdf", Text: "Ada Lovelace"}, {Source: "/docs/passport.pdf", Text: "Born 1815-12-10, no. X1234567"}}
	res, err := AIFill(bg, db, "p1", facts, complete)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, f := range res.Filled {
		keys = append(keys, f.Key+"<"+f.Origin)
	}
	// title "Countess" is not an option; gender "female" is, case-blind.
	if want := "gender<cv.pdf,last_name<cv.pdf,passport_number<passport.pdf,date_of_birth<passport.pdf"; len(keys) != 4 || !sameSet(keys, strings.Split(want, ",")) {
		t.Errorf("filled %v", keys)
	}
	for _, banned := range []string{`"key":"card_number"`, `"key":"portrait_photo"`, `"key":"first_name"`} {
		if strings.Contains(prompt, banned) {
			t.Errorf("the model was asked about %s", banned)
		}
	}
	got, _ := Get(bg, db, "p1", true)
	vals := map[string]Entry{}
	for _, e := range got {
		vals[e.Key] = e
	}
	if vals["first_name"].Value != "Typed" || vals["first_name"].Source != SourceUser {
		t.Errorf("the person's value changed: %+v", vals["first_name"])
	}
	if vals["gender"].Value != "Female" || vals["gender"].Source != SourceAI || vals["gender"].Origin != "cv.pdf" {
		t.Errorf("gender: %+v", vals["gender"])
	}
	if vals["passport_number"].Value != "X1234567" {
		t.Errorf("an ID number is filled (and stored encrypted): %+v", vals["passport_number"])
	}
	if _, ok := vals["card_number"]; ok {
		t.Error("the card was filled")
	}
	if res.Skipped != res.Asked-4 {
		t.Errorf("skipped %d of %d", res.Skipped, res.Asked)
	}
}

func TestAIFillWithNothingToGoOnDoesNotAskTheModel(t *testing.T) {
	db, _ := newDB(t)
	asked := false
	res, err := AIFill(bg, db, "p1", nil, func(context.Context, string) (string, error) { asked = true; return "", nil })
	if err != nil || asked || len(res.Filled) != 0 {
		t.Errorf("res %+v, err %v, asked %v", res, err, asked)
	}
}

func TestAIFillReportsAModelFailureAndAnUnusableAnswer(t *testing.T) {
	db, _ := newDB(t)
	facts := []Fact{{Source: "a", Text: "x"}}
	if _, err := AIFill(bg, db, "p1", facts, func(context.Context, string) (string, error) { return "", errors.New("down") }); err == nil {
		t.Error("a model error was swallowed")
	}
	if _, err := AIFill(bg, db, "p1", facts, func(context.Context, string) (string, error) { return "no json", nil }); err == nil {
		t.Error("an unusable answer was accepted")
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}
