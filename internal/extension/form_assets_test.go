package extension

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

type fakeAssets struct {
	fakeFiller
	card    CardDetails
	hasCard bool
	photos  map[string]PhotoFile
	asked   []string
}

func (f *fakeAssets) Card(context.Context, string) (CardDetails, bool, error) {
	return f.card, f.hasCard, nil
}

func (f *fakeAssets) Photo(_ context.Context, _, key string) (PhotoFile, bool, error) {
	f.asked = append(f.asked, key)
	p, ok := f.photos[key]
	return p, ok, nil
}

func cardFields() []any {
	return []any{
		fld("num", "Card number", "text", map[string]any{"autocomplete": "cc-number"}),
		fld("name", "Name on card", "text", nil),
		fld("exp", "Expiry date", "text", map[string]any{"placeholder": "MM / YYYY"}),
		fld("cvv", "CVV", "text", nil),
		fld("first", "First name", "text", nil), // not a card field
	}
}

func TestFormCardFillsOnlyCardFieldsFromTheStoredCard(t *testing.T) {
	a := &fakeAssets{card: CardDetails{Holder: "Ada Lovelace", Number: "4242 4242-4242 4242", Expiry: "01/27", CVV: " 123 "}, hasCard: true}
	srv := fillSrv(a)
	got, err := callChat(t, srv, MethodFormCard, map[string]any{"profile": "p1", "fields": cardFields()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := got.(*CardValuesReply)
	want := map[string]string{"num": "4242424242424242", "name": "Ada Lovelace", "exp": "01/2027", "cvv": "123"}
	g := map[string]string{}
	for _, v := range r.Values {
		g[v.ID] = v.Value
	}
	if !r.Saved || len(g) != 4 {
		t.Fatalf("%+v", r)
	}
	for k, v := range want {
		if g[k] != v {
			t.Errorf("%s = %q, want %q", k, g[k], v)
		}
	}
}

func TestFormCardExpiryShapes(t *testing.T) {
	card := CardDetails{Expiry: "1/27"}
	cases := []struct {
		f    FormField
		want string
	}{
		{FormField{Autocomplete: "cc-exp-month"}, "01"},
		{FormField{Autocomplete: "cc-exp-year"}, "2027"},
		{FormField{Autocomplete: "cc-exp-year", MaxLength: 2}, "27"},
		{FormField{Autocomplete: "cc-exp", MaxLength: 4}, "0127"},
		{FormField{Autocomplete: "cc-exp"}, "01/27"},
		{FormField{Name: "expiryMonth", Options: []string{"01", "02", "12"}}, "01"},
		{FormField{Name: "expiry_year", Options: []string{"26", "27", "28"}}, "27"},
		{FormField{Name: "expiry_year", Options: []string{"2026", "2027"}}, "2027"},
	}
	for _, c := range cases {
		role := cardRoleOf(c.f)
		if got := cardValue(c.f, role, card); got != c.want {
			t.Errorf("%+v: %q, want %q", c.f, got, c.want)
		}
	}
	if cardValue(FormField{Autocomplete: "cc-exp"}, roleExpiry, CardDetails{Expiry: "13/27"}) != "" {
		t.Error("month 13 was accepted")
	}
}

func TestFormCardWithNoSavedCardReturnsNothing(t *testing.T) {
	got, err := callChat(t, fillSrv(&fakeAssets{}), MethodFormCard, map[string]any{"profile": "p1", "fields": cardFields()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := got.(*CardValuesReply); r.Saved || len(r.Values) != 0 {
		t.Errorf("%+v", r)
	}
}

func TestFormFillStillWithholdsCardFieldsFromTheModel(t *testing.T) {
	f := &fakeAssets{fakeFiller: fakeFiller{facts: []FormFact{{Source: "a", Text: "x"}}, answer: `{"values":{}}`}}
	if _, err := callChat(t, fillSrv(f), MethodFormFill, map[string]any{"profile": "p1", "fields": cardFields()}, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{`"num"`, `"cvv"`} {
		if strings.Contains(f.prompt, id) {
			t.Errorf("a card field reached the model: %s", id)
		}
	}
}

func TestFormPhotosMatchByLabelAndAcceptList(t *testing.T) {
	a := &fakeAssets{photos: map[string]PhotoFile{
		"portrait_photo": {Name: "portrait_photo.jpg", Mime: "image/jpeg", Data: []byte("jpeg")},
		"passport_photo": {Name: "passport_photo.pdf", Mime: "application/pdf", Data: []byte("pdf")},
	}}
	got, err := callChat(t, fillSrv(a), MethodFormPhotos, map[string]any{
		"profile": "p1",
		"fields": []any{
			fld("a", "Upload your photo", "file", map[string]any{"accept": "image/*"}),
			fld("b", "Passport scan", "file", map[string]any{"accept": "image/*"}), // a pdf is not an image
			fld("c", "Passport scan", "file", map[string]any{"accept": ".pdf,.png"}),
			fld("d", "Cover letter", "file", nil),
			fld("e", "ID card (front)", "file", nil), // none stored
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := got.(*PhotoReply)
	if len(r.Files) != 2 || r.Files[0].ID != "a" || r.Files[0].Key != "portrait_photo" || r.Files[1].ID != "c" {
		t.Fatalf("%+v", r.Files)
	}
	if b, _ := base64.StdEncoding.DecodeString(r.Files[0].Data); string(b) != "jpeg" || r.Files[0].Mime != "image/jpeg" {
		t.Errorf("file a: %+v", r.Files[0])
	}
}

func TestPhotoKeyRules(t *testing.T) {
	for label, want := range map[string]string{
		"Proof of address (utility bill)": "proof_of_address_photo",
		"Signature":                       "signature_photo",
		"Passport copy":                   "passport_photo",
		"Driver's licence":                "drivers_license_photo",
		"ID card back":                    "id_back_photo",
		"National ID card - front":        "id_front_photo",
		"Profile picture":                 "portrait_photo",
		"Passport-size photo":             "passport_photo", // "passport" wins: the more specific document
		"Resume":                          "",
	} {
		if got := photoKeyFor(FormField{Label: label}); got != want {
			t.Errorf("%q -> %q, want %q", label, got, want)
		}
	}
}
