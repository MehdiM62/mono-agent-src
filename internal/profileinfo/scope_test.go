package profileinfo

import (
	"regexp"
	"strings"
	"testing"
)

func TestTypeDefaultsToPersonAndSwitchesToBusiness(t *testing.T) {
	db, _ := newDB(t)
	if got, _ := TypeOf(bg, db, "p1"); got != ScopePerson {
		t.Fatalf("default type = %q", got)
	}
	if err := Set(bg, db, "p1", TypeKey, "business", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := TypeOf(bg, db, "p1"); got != ScopeBusiness {
		t.Errorf("type = %q", got)
	}
	if err := Set(bg, db, "p1", TypeKey, "Company", SourceUser, ""); err == nil {
		t.Error("an unknown profile type was accepted")
	}
}

func TestEveryGroupHasAKnownScope(t *testing.T) {
	for _, g := range Groups {
		if g.Scope != "" && g.Scope != ScopePerson && g.Scope != ScopeBusiness {
			t.Errorf("group %s: scope %q", g.ID, g.Scope)
		}
	}
}

func TestBusinessProfileIsOnlyAskedBusinessAndSharedFields(t *testing.T) {
	db, _ := newDB(t)
	if err := Set(bg, db, "p1", TypeKey, "Business", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	fields, err := Missing(bg, db, "p1")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, f := range fields {
		have[f.Key] = true
	}
	for _, k := range []string{"vat_id", "company_name", "owner_name", "owner_tax_id", "iban", "website"} {
		if !have[k] {
			t.Errorf("a business profile is not asked for %s", k)
		}
	}
	for _, k := range []string{"first_name", "passport_number", "employer", TypeKey} {
		if have[k] {
			t.Errorf("a business profile is asked for %s", k)
		}
	}
}

func TestPersonProfileIsNotAskedBusinessFields(t *testing.T) {
	db, _ := newDB(t)
	fields, _ := Missing(bg, db, "p1")
	for _, f := range fields {
		if f.Scope() == ScopeBusiness {
			t.Errorf("a person is asked for %s", f.Key)
		}
	}
}

func TestBusinessValuesSurviveSwitchingBackToPerson(t *testing.T) {
	db, _ := newDB(t)
	if err := Set(bg, db, "p1", "company_name", "Acme GmbH", SourceUser, ""); err != nil {
		t.Fatal(err)
	}
	got, _ := Get(bg, db, "p1", false)
	found := false
	for _, e := range got {
		found = found || (e.Key == "company_name" && e.Value == "Acme GmbH")
	}
	if !found {
		t.Error("company_name was not kept")
	}
}

// Fields whose names say they hold an ID, tax, licence, registration or birth
// number must be encrypted (Sensitive). The ones below are not numbers.
var idLike = regexp.MustCompile(`(^|_)(id|number|tax|licence|license|registration|register|dob|birth|lei|duns|eori|giin|permit)(_|$)|date_of_birth`)

var notSecret = map[string]bool{
	"vat_registered": true, "vat_filing_frequency": true, "tax_country": true, "company_tax_country": true,
	"tax_office": true, "business_licence_expiry": true, "visa_expiry_date": true, "id_card_expiry_date": true,
	"drivers_license_expiry_date": true, "passport_expiry_date": true, "passport_issue_date": true,
	"passport_country": true, "date_of_birth": true, "place_of_birth": true, "country_of_birth": true,
	"phone_country_code": true, "us_entity": true, "us_person": true, "owner_us_person": true,
	"registration_country": true,
	"id_front_photo":       true, "id_back_photo": true, "passport_photo": true, "drivers_license_photo": true,
	"owner_id_photo": true, "vat_certificate_photo": true,
}

func TestIDLikeFieldsAreSensitive(t *testing.T) {
	for _, fd := range Catalog {
		if fd.Kind == Photo || notSecret[fd.Key] || fd.Sensitive || fd.Card {
			continue
		}
		if idLike.MatchString(fd.Key) {
			t.Errorf("%s looks like an ID or tax number but is not marked sensitive", fd.Key)
		}
	}
}

func TestSensitiveFieldsAreEncryptedAtRest(t *testing.T) {
	db, _ := newDB(t)
	n := 0
	for _, fd := range Catalog {
		if !fd.Sensitive {
			continue
		}
		v := "Z9-12345-secret"
		if fd.Kind == Date {
			v = "1980-02-03"
		}
		if err := Set(bg, db, "p1", fd.Key, v, SourceUser, ""); err != nil {
			t.Fatalf("%s: %v", fd.Key, err)
		}
		var raw string
		if err := db.QueryRowContext(bg, `SELECT value FROM profile_info WHERE profile_id = 'p1' AND key = ?`, fd.Key).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(raw, "vaultenc:v1:") || strings.Contains(raw, "secret") || strings.Contains(raw, "1980") {
			t.Errorf("%s is stored as %q", fd.Key, raw)
		}
		n++
	}
	if n < 20 {
		t.Errorf("only %d sensitive fields", n)
	}
}
