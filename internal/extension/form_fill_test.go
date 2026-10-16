package extension

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

type fakeFiller struct {
	facts   []FormFact
	answer  string
	factErr error
	prompt  string
	queries []string
	runtime string
	info    []InfoValue
	infoErr error
	calls   int // Complete calls
}

func (f *fakeFiller) Facts(_ context.Context, _ string, q []string) ([]FormFact, error) {
	f.queries = q
	return f.facts, f.factErr
}

func (f *fakeFiller) Info(context.Context, string) ([]InfoValue, error) { return f.info, f.infoErr }

func (f *fakeFiller) Complete(_ context.Context, runtime, _ string, prompt string) (string, error) {
	f.calls++
	f.runtime, f.prompt = runtime, prompt
	return f.answer, nil
}

func fillSrv(f FormFiller) *Server {
	srv := NewServer("127.0.0.1:0", zerolog.Nop())
	srv.SetFormFiller(f)
	return srv
}

func fld(id, label, typ string, extra map[string]any) map[string]any {
	m := map[string]any{"id": id, "label": label, "type": typ, "name": id}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestFormFillValuesAreCheckedAgainstTheirFields(t *testing.T) {
	f := &fakeFiller{
		facts: []FormFact{{Source: "/docs/passport.pdf", Text: "Name: Ada Lovelace. Born 1815."}},
		answer: "```json\n" + `{"values":{
			"first":{"value":"Ada","fact":1},
			"title":{"value":"ms.","fact":1},
			"country":{"value":"Atlantis","fact":1},
			"ghost":{"value":"x","fact":1},
			"bio":{"value":"` + strings.Repeat("a", 400) + `","fact":1}}}` + "\n```",
	}
	got, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{
		"profile": "p1",
		"fields": []any{
			fld("first", "First Name", "text", nil),
			fld("title", "Title", "select", map[string]any{"options": []any{"Mr.", "Ms.", "Dr."}}),
			fld("country", "Country", "select", map[string]any{"options": []any{"Germany", "Ireland"}}),
			fld("bio", "Bio", "text", nil),
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := got.(*FormFillReply)
	if len(r.Values) != 2 {
		t.Fatalf("want first+title, got %+v", r.Values)
	}
	if r.Values[0] != (FormValue{ID: "first", Value: "Ada", Source: "passport.pdf"}) {
		t.Errorf("first: %+v", r.Values[0])
	}
	if r.Values[1].Value != "Ms." {
		t.Errorf("an option is matched case-blind and returned as listed: %+v", r.Values[1])
	}
	if r.Skipped != 2 {
		t.Errorf("skipped = %d, want 2", r.Skipped)
	}
}

func TestFormFillNeverShowsSecretFieldsToTheModel(t *testing.T) {
	f := &fakeFiller{facts: []FormFact{{Source: "a.txt", Text: "Ada"}}, answer: `{"values":{}}`}
	got, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{
		"profile": "p1",
		"fields": []any{
			fld("pw", "Password", "password", nil),
			fld("cc", "Card number", "text", nil),
			fld("code", "Code", "text", map[string]any{"autocomplete": "one-time-code"}),
			fld("agree", "I agree", "checkbox", nil),
			fld("first", "First name", "text", nil),
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.(*FormFillReply).Skipped != 5 {
		t.Errorf("skipped = %d, want 5 (four withheld, one unanswered)", got.(*FormFillReply).Skipped)
	}
	for _, id := range []string{`"pw"`, `"cc"`, `"code"`, `"agree"`} {
		if strings.Contains(f.prompt, id) {
			t.Errorf("prompt mentions %s", id)
		}
	}
	if !strings.Contains(f.prompt, `"first"`) {
		t.Error("prompt lacks the fillable field")
	}
}

func TestFormFillWithNoFactsDoesNotAskTheModel(t *testing.T) {
	f := &fakeFiller{answer: "should not be used"}
	got, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{
		"profile": "p1", "fields": []any{fld("first", "First name", "text", nil)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := got.(*FormFillReply); len(r.Values) != 0 || r.Skipped != 1 {
		t.Errorf("%+v", r)
	}
	if f.prompt != "" {
		t.Error("the model was asked with nothing to go on")
	}
}

func TestFormFillIDQueryOnlyWhenFormAsksForIDs(t *testing.T) {
	f := &fakeFiller{facts: []FormFact{{Source: "a", Text: "x"}}, answer: `{"values":{}}`}
	srv := fillSrv(f)
	call := func(label string) {
		if _, err := callChat(t, srv, MethodFormFill, map[string]any{
			"profile": "p1", "fields": []any{fld("f", label, "text", nil)},
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	call("First name")
	n := len(f.queries)
	call("Tax identification number")
	if len(f.queries) != n+1 {
		t.Errorf("queries %d then %d", n, len(f.queries))
	}
}

func TestFormFillRefusals(t *testing.T) {
	srv := fillSrv(&fakeFiller{})
	for name, p := range map[string]map[string]any{
		"no profile":  {"fields": []any{fld("a", "A", "text", nil)}},
		"no fields":   {"profile": "p1"},
		"bad id":      {"profile": "p1", "fields": []any{fld("a b", "A", "text", nil)}},
		"dup id":      {"profile": "p1", "fields": []any{fld("a", "A", "text", nil), fld("a", "B", "text", nil)}},
		"bad runtime": {"profile": "p1", "runtime": "x y", "fields": []any{fld("a", "A", "text", nil)}},
	} {
		if _, err := callChat(t, srv, MethodFormFill, p, nil); errCode(err) != CodeInvalidInput {
			t.Errorf("%s: %v", name, err)
		}
	}
	bad := &fakeFiller{facts: []FormFact{{Source: "a", Text: "x"}}, answer: "no json here"}
	if _, err := callChat(t, fillSrv(bad), MethodFormFill, map[string]any{
		"profile": "p1", "fields": []any{fld("a", "A", "text", nil)},
	}, nil); errCode(err) != CodeUnavailable {
		t.Errorf("unusable answer: %v", err)
	}
	down := &fakeFiller{factErr: errors.New("monomind missing")}
	if _, err := callChat(t, fillSrv(down), MethodFormFill, map[string]any{
		"profile": "p1", "fields": []any{fld("a", "A", "text", nil)},
	}, nil); errCode(err) != CodeUnavailable {
		t.Errorf("search down: %v", err)
	}
}

var testInfo = []InfoValue{
	{Key: "first_name", Label: "First name", Kind: "text", Autocomplete: "given-name", Value: "Ada"},
	{Key: "last_name", Label: "Last name", Kind: "text", Autocomplete: "family-name", Value: "Lovelace"},
	{Key: "title", Label: "Title", Kind: "select", Value: "Ms."},
	{Key: "email", Label: "Email", Kind: "email", Autocomplete: "email", Value: "ada@example.com"},
	{Key: "phone", Label: "Mobile phone", Kind: "phone", Autocomplete: "tel", Value: "+49 1234"},
	{Key: "country", Label: "Country", Kind: "country", Autocomplete: "country-name", Value: "Germany"},
	{Key: "date_of_birth", Label: "Date of birth", Kind: "date", Autocomplete: "bday", Value: "1815-12-10"},
	{Key: "passport_number", Label: "Passport number", Kind: "text", Sensitive: true, Value: "X1234567"},
}

func vals(r *FormFillReply) map[string]string {
	m := map[string]string{}
	for _, v := range r.Values {
		m[v.ID] = v.Value + "@" + v.Source
	}
	return m
}

func TestFormFillInfoAnswersExactlyWithNoModel(t *testing.T) {
	f := &fakeFiller{info: testInfo, answer: "must not be used"}
	got, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{
		"profile": "p1",
		"fields": []any{
			fld("a", "First Name", "text", map[string]any{"name": "firstName"}),
			fld("b", "Surname *", "text", nil),
			fld("c", "Title", "select", map[string]any{"options": []any{"Mr.", "Ms.", "Dr."}}),
			fld("d", "Where do we write?", "email", nil),
			fld("e", "Ring me", "tel", nil),
			fld("f", "Land", "select", map[string]any{"autocomplete": "billing country-name", "options": []any{"Austria", "DE - Germany"}}),
			fld("g", "Passport number", "text", nil),
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"a": "Ada@Profile info", "b": "Lovelace@Profile info", "c": "Ms.@Profile info", "d": "ada@example.com@Profile info",
		"e": "+49 1234@Profile info", "f": "DE - Germany@Profile info", "g": "X1234567@Profile info",
	}
	g := vals(got.(*FormFillReply))
	for k, v := range want {
		if g[k] != v {
			t.Errorf("%s = %q, want %q", k, g[k], v)
		}
	}
	if f.calls != 0 || f.queries != nil {
		t.Errorf("nothing was left to ask: model calls %d, searches %v", f.calls, f.queries)
	}
}

func TestFormFillSendsTheModelTheRestButNeverIDNumbers(t *testing.T) {
	f := &fakeFiller{
		info:   testInfo,
		facts:  []FormFact{{Source: "/docs/cv.pdf", Text: "Lives at 1 Analytical Way"}},
		answer: `{"values":{"x":{"value":"1 Analytical Way","fact":1},"y":{"value":"10/12/1815","fact":2}}}`,
	}
	got, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{
		"profile": "p1",
		"fields": []any{
			fld("n", "First name", "text", nil),
			fld("x", "Street and number", "text", nil),
			fld("y", "Date of birth (DD/MM/YYYY)", "text", nil), // a date in a text box: a layout to decide
			fld("p", "Passport number", "text", nil),
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	g := vals(got.(*FormFillReply))
	if g["n"] != "Ada@Profile info" || g["p"] != "X1234567@Profile info" || g["x"] != "1 Analytical Way@cv.pdf" || g["y"] != "10/12/1815@Profile info" {
		t.Errorf("values %v", g)
	}
	if strings.Contains(f.prompt, "X1234567") || strings.Contains(f.prompt, `"n"`) || strings.Contains(f.prompt, `"p"`) {
		t.Errorf("the prompt carries an ID number or an already-answered field:\n%s", f.prompt)
	}
	if !strings.Contains(f.prompt, "First name: Ada") || !strings.Contains(f.prompt, "Lives at 1 Analytical Way") {
		t.Errorf("the prompt lacks the info or the documents:\n%s", f.prompt)
	}
}

func TestFormFillWorksWithoutInfoAndWhenInfoFails(t *testing.T) {
	f := &fakeFiller{infoErr: errors.New("no table"), facts: []FormFact{{Source: "a.txt", Text: "Ada"}}, answer: `{"values":{"a":{"value":"Ada","fact":1}}}`}
	got, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{
		"profile": "p1", "fields": []any{fld("a", "Name of person", "text", nil)},
	}, nil)
	if err != nil || vals(got.(*FormFillReply))["a"] != "Ada@a.txt" {
		t.Errorf("%v %v", got, err)
	}
}

func TestFormFillKeepsInfoValuesWhenTheModelFails(t *testing.T) {
	f := &fakeFiller{info: testInfo, facts: []FormFact{{Source: "a", Text: "x"}}, answer: "no json"}
	got, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{
		"profile": "p1", "fields": []any{fld("a", "First name", "text", nil), fld("b", "Something odd", "text", nil)},
	}, nil)
	if err != nil {
		t.Fatalf("an exact answer was thrown away: %v", err)
	}
	if r := got.(*FormFillReply); len(r.Values) != 1 || r.Values[0].ID != "a" || r.Skipped != 1 {
		t.Errorf("%+v", r)
	}
}
